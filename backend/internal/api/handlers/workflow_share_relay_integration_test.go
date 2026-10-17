package handlers_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestShareRelaySeparatesVisitorsBehindOneFrontend(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)
	share, _ := createShare(t, d, wf.ID, owner, `{}`)
	d.ShareReadRelayKey = []byte(strings.Repeat("r", 32))
	path := "/shares/" + share.Token

	read := func(visitor string, signed bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "10.0.0.8:4123"
		r.Header.Set("X-Forwarded-For", "203.0.113.190")
		if visitor != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			r.Header.Set("X-AgentMesh-Share-Visitor", visitor)
			r.Header.Set("X-AgentMesh-Share-Timestamp", timestamp)
			if signed {
				mac := hmac.New(sha256.New, d.ShareReadRelayKey)
				_, _ = mac.Write([]byte(strings.Join([]string{
					"v1", http.MethodGet, path, visitor, timestamp,
				}, "\n")))
				r.Header.Set("X-AgentMesh-Share-Signature", hex.EncodeToString(mac.Sum(nil)))
			}
		}
		r = withURLParam(r, "token", share.Token)
		w := httptest.NewRecorder()
		d.GetShare(w, r)
		return w
	}

	for i := 0; i < 60; i++ {
		if w := read("198.51.100.191", true); w.Code != http.StatusOK {
			t.Fatalf("first visitor read %d = %d", i+1, w.Code)
		}
	}
	if w := read("198.51.100.191", true); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("exhausted visitor = %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := read("198.51.100.192", true); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), share.Name) {
		t.Fatalf("independent visitor lost share after same-egress exhaustion: %d %s", w.Code, w.Body.String())
	}
	if w := read("198.51.100.193", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned address = %d", w.Code)
	}
	for i := 0; i < 60; i++ {
		if w := read("", false); w.Code != http.StatusOK {
			t.Fatalf("ordinary direct read %d = %d", i+1, w.Code)
		}
	}
	if w := read("", false); w.Code != http.StatusTooManyRequests {
		t.Fatalf("ordinary direct limiter was bypassed: %d", w.Code)
	}
	if w := read("198.51.100.192", true); w.Code != http.StatusOK {
		t.Fatalf("direct egress exhaustion affected signed visitor: %d", w.Code)
	}
}
