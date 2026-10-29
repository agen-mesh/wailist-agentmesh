package merchant

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testBearer = "merchant-unit-only-bearer-not-deployed-123456"

func handlerFor(t *testing.T, f *fixture) http.Handler {
	t.Helper()
	h, err := NewHandler(f.svc, testBearer)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func requestHTTP(t *testing.T, h http.Handler, method, path string, input any, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if raw, ok := input.([]byte); ok {
		body = raw
	} else if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if authorized {
		r.Header.Set("Authorization", "Bearer "+testBearer)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
	return w
}

func TestHTTPQuoteAcceptFulfillResult(t *testing.T) {
	f := newFixture(t)
	h := handlerFor(t, f)
	w := requestHTTP(t, h, "POST", "/merchant/quote", QuoteRequest{Method: "POST", Resource: f.cfg.Resource, Request: f.snap.Request}, true)
	if w.Code != 200 {
		t.Fatal("quote", w.Code, w.Body.String())
	}
	var q Quote
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	snap := makeSnapshot(t, q, "http-order", f.snap.Request, f.now)
	w = requestHTTP(t, h, "POST", "/merchant/accept", AcceptRequest{Snapshot: snap, Quote: q}, true)
	if w.Code != 200 {
		t.Fatal("accept", w.Code, w.Body.String())
	}
	for _, private := range []string{"private text", "canonicalIntent", "account-1", testBearer} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private acceptance data exposed")
		}
	}
	w = requestHTTP(t, h, "POST", "/merchant/fulfill", FulfillRequest{OrderID: snap.OrderID, IntentHash: snap.IntentHash, Payment: f.event}, true)
	if w.Code != 200 {
		t.Fatal("fulfill", w.Code, w.Body.String())
	}
	var view ResultView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != Completed || !view.Paid || view.ReconciliationRequired || len(view.Result) == 0 || f.calls.Load() != 1 {
		t.Fatal("incorrect completed result", view.State)
	}
	first := bytes.Clone(view.Result)
	w = requestHTTP(t, h, "GET", "/merchant/results/http-order?intentHash="+snap.IntentHash, nil, true)
	if w.Code != 200 {
		t.Fatal("result", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, view.Result) || f.calls.Load() != 1 {
		t.Fatal("result lookup executed or changed result")
	}
	w = requestHTTP(t, h, "POST", "/merchant/fulfill", FulfillRequest{OrderID: snap.OrderID, IntentHash: snap.IntentHash, Payment: f.event}, true)
	if w.Code != 200 || f.calls.Load() != 1 {
		t.Fatal("duplicate delivery reexecuted")
	}
}

func TestHTTPBoundaryControls(t *testing.T) {
	f := newFixture(t)
	h := handlerFor(t, f)
	for _, path := range []string{"/merchant/quote", "/merchant/accept", "/merchant/fulfill", "/merchant/results/order-1?intentHash=" + f.snap.IntentHash} {
		w := requestHTTP(t, h, "POST", path, []byte(`{}`), false)
		if w.Code != http.StatusUnauthorized {
			t.Fatal("missing auth accepted", path, w.Code)
		}
	}
	for _, authorization := range []string{"Bearer wrong", "bearer " + testBearer, "Bearer " + testBearer + " "} {
		r := httptest.NewRequest("POST", "/merchant/quote", strings.NewReader("{}"))
		r.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("wrong bearer accepted")
		}
	}
	r := httptest.NewRequest("GET", "/merchant/results/order-1?intentHash="+f.snap.IntentHash, nil)
	r.Header.Add("Authorization", "Bearer "+testBearer)
	r.Header.Add("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate auth accepted")
	}
	cases := []struct {
		name, method, path string
		raw                []byte
		status             int
	}{
		{"unknown_private", "POST", "/merchant/quote", []byte(`{"privateKey":"never-echo"}`), 400},
		{"duplicate", "POST", "/merchant/quote", []byte(`{"method":"GET","method":"POST"}`), 400},
		{"oversized", "POST", "/merchant/quote", []byte(strings.Repeat("x", 2*MaxRequestBytes+1)), 413},
		{"wrong_method", "GET", "/merchant/quote", nil, 405},
		{"missing_order", "GET", "/merchant/results/missing?intentHash=" + f.snap.IntentHash, nil, 404},
		{"duplicate_query", "GET", "/merchant/results/order-1?intentHash=" + f.snap.IntentHash + "&intentHash=" + f.snap.IntentHash, nil, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := requestHTTP(t, h, c.method, c.path, c.raw, true)
			if w.Code != c.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "never-echo") {
				t.Fatal("private error leaked")
			}
		})
	}
	if f.calls.Load() != 0 || len(f.store.rows) != 0 {
		t.Fatal("invalid HTTP executed or accepted")
	}
	if _, err := NewHandler(f.svc, "short"); err == nil {
		t.Fatal("weak internal bearer accepted")
	}
}

func TestHTTPPendingStartedAndUnknown(t *testing.T) {
	for _, state := range []State{Pending, Started, Unknown} {
		t.Run(string(state), func(t *testing.T) {
			f := newFixture(t)
			f.accept(t)
			if state == Pending {
				f.svc.verify = func(context.Context, Snapshot, PaymentEvent) (PaymentProof, error) {
					return PaymentProof{}, ErrPaymentPending
				}
			} else {
				f.store.mu.Lock()
				row := f.store.rows[f.snap.OrderID]
				row.State = state
				row.Payment = &f.event
				f.store.rows[f.snap.OrderID] = row
				f.store.mu.Unlock()
			}
			h := handlerFor(t, f)
			w := requestHTTP(t, h, "POST", "/merchant/fulfill", FulfillRequest{OrderID: f.snap.OrderID, IntentHash: f.snap.IntentHash, Payment: f.event}, true)
			if w.Code != 202 || f.calls.Load() != 0 {
				t.Fatal("uncertain state executed", w.Code)
			}
			if state != Pending && !strings.Contains(w.Body.String(), `"reconciliationRequired":true`) {
				t.Fatal("uncertainty hidden", w.Body.String())
			}
			if bytes.Contains(w.Body.Bytes(), []byte("private text")) {
				t.Fatal("private payload leaked")
			}
		})
	}
}

type lostResponseWriter struct{ header http.Header }

func (w *lostResponseWriter) Header() http.Header     { return w.header }
func (*lostResponseWriter) WriteHeader(int)           {}
func (*lostResponseWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestHTTPLostResponseLookup(t *testing.T) {
	f := newFixture(t)
	f.accept(t)
	h := handlerFor(t, f)
	b, _ := json.Marshal(FulfillRequest{OrderID: f.snap.OrderID, IntentHash: f.snap.IntentHash, Payment: f.event})
	r := httptest.NewRequest("POST", "/merchant/fulfill", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+testBearer)
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(&lostResponseWriter{header: make(http.Header)}, r)
	w := requestHTTP(t, h, "GET", "/merchant/results/order-1?intentHash="+f.snap.IntentHash, nil, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"completed"`) || f.calls.Load() != 1 {
		t.Fatal("lost response cannot be recovered", w.Code, w.Body.String())
	}
}
