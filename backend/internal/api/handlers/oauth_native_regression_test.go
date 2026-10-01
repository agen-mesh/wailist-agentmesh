package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

func oauthTestStore(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestNativeStartURLKeepsStateOnCallbackHost(t *testing.T) {
	d := configuredDeps()
	router := chi.NewRouter()
	router.Get("/auth/oauth/{provider}/url", d.OAuthStartURL)
	router.Get("/api/auth/oauth/{provider}", d.OAuthStart)
	router.Get("/api/auth/oauth/{provider}/callback", d.OAuthCallback)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, d.BaseURL+"/auth/oauth/google/url", nil))
	var body struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	startURL, err := url.Parse(body.URL + "?client=android&challenge=" + challengeOf("verifier"))
	if err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, startURL.String(), nil))
	provider, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callbackURL, err := url.Parse(provider.Query().Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	if startURL.Host != callbackURL.Host || startURL.Host == "api.test" {
		t.Fatal("start and callback must share the frontend host")
	}
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(startURL, start.Result().Cookies())
	q := callbackURL.Query()
	q.Set("state", provider.Query().Get("state"))
	callbackURL.RawQuery = q.Encode()
	req := httptest.NewRequest(http.MethodGet, callbackURL.String(), nil)
	for _, cookie := range jar.Cookies(callbackURL) {
		req.AddCookie(cookie)
	}
	callback := httptest.NewRecorder()
	router.ServeHTTP(callback, req)
	location, _ := url.Parse(callback.Header().Get("Location"))
	if location.Query().Get("error") != "no_code" {
		t.Fatalf("state did not validate: %s", location)
	}
}

func TestExchangeCodeIsConsumedAcrossBackendInstances(t *testing.T) {
	first, second := testDeps(), testDeps()
	first.Store, second.Store = oauthTestStore(t), oauthTestStore(t)
	verifier := "saved-verifier"
	code, err := first.issueExchangeCode("user-1", "test@example.test", challengeOf(verifier))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":"` + code + `","verifier":"` + verifier + `"}`
	const attempts = 16
	results := make(chan int, attempts)
	var group sync.WaitGroup
	for i := range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			d := first
			if i%2 == 1 {
				d = second
			}
			results <- postExchange(t, d, body).Code
		}()
	}
	group.Wait()
	close(results)
	successes := 0
	for code := range results {
		if code == http.StatusOK {
			successes++
		} else if code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d", code)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful exchanges, want one", successes)
	}
	if rec := postExchange(t, second, body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sequential replay: %d", rec.Code)
	}
	consumed, err := first.Store.ConsumeOAuthExchangeCode(context.Background(), "expired", time.Now().Add(-time.Minute))
	if err != nil || consumed {
		t.Fatalf("expired redemption: consumed=%v err=%v", consumed, err)
	}
}

func TestWrongVerifierDoesNotConsumeExchangeCode(t *testing.T) {
	d := testDeps()
	d.Store = oauthTestStore(t)
	code, err := d.issueExchangeCode("user-1", "test@example.test", challengeOf("correct"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := postExchange(t, d, `{"code":"`+code+`","verifier":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong verifier: %d", rec.Code)
	}
	if rec := postExchange(t, d, `{"code":"`+code+`","verifier":"correct"}`); rec.Code != http.StatusOK {
		t.Fatalf("valid verifier: %d", rec.Code)
	}
}

func TestExchangeRejectsEquivalentSignatureEncoding(t *testing.T) {
	d := testDeps()
	d.Store = oauthTestStore(t)
	code, err := d.issueExchangeCode("user-1", "test@example.test", challengeOf("correct"))
	if err != nil {
		t.Fatal(err)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, code[len(code)-1])
	alias := code[:len(code)-1] + string(alphabet[last|1])
	if rec := postExchange(t, d, `{"code":"`+code+`","verifier":"correct"}`); rec.Code != http.StatusOK {
		t.Fatalf("first exchange: %d", rec.Code)
	}
	if rec := postExchange(t, d, `{"code":"`+alias+`","verifier":"correct"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("encoded replay: %d", rec.Code)
	}
}
