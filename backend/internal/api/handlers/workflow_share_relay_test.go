package handlers

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

func signShareRelayRequest(r *http.Request, visitor string, seconds int64, key []byte) {
	timestamp := strconv.FormatInt(seconds, 10)
	r.Header.Set(shareVisitorHeader, visitor)
	r.Header.Set(shareTimestampHeader, timestamp)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.Join([]string{
		"v1", r.Method, r.URL.EscapedPath(), visitor, timestamp,
	}, "\n")))
	r.Header.Set(shareSignatureHeader, hex.EncodeToString(mac.Sum(nil)))
}

func TestShareRelayKeyConfiguration(t *testing.T) {
	for _, raw := range []string{"bad", strings.Repeat("ab", 31), strings.Repeat("ab", 33), strings.Repeat("zz", 32)} {
		if _, err := ParseShareReadRelayKey(raw); err == nil || strings.Contains(err.Error(), raw) {
			t.Fatalf("malformed key must fail without echoing it")
		}
	}
	key, err := ParseShareReadRelayKey("")
	if err != nil || len(key) != 0 {
		t.Fatalf("unset relay: key length %d, error %v", len(key), err)
	}
	key, err = ParseShareReadRelayKey(strings.Repeat("01", 32))
	if err != nil || len(key) != 32 {
		t.Fatalf("configured relay: key length %d, error %v", len(key), err)
	}
}

func TestShareRelayContractVector(t *testing.T) {
	key, err := ParseShareReadRelayKey(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/shares/contract-token", nil)
	r.Header.Set(shareVisitorHeader, "198.51.100.7")
	r.Header.Set(shareTimestampHeader, "1792244800")
	r.Header.Set(shareSignatureHeader, "86fb6cb9f2bd460726bf21b54cc2b6559b7e9b33c463e57eb178d5768bee5a60")
	d := &Deps{ShareReadRelayKey: key}
	got, err := d.shareReadClientIP(r, time.Unix(1792244800, 0))
	if err != nil || got != "198.51.100.7" {
		t.Fatalf("cross-runtime signature: address %q, error %v", got, err)
	}
}

func TestShareRelayAuthentication(t *testing.T) {
	key, err := ParseShareReadRelayKey(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name   string
		change func(*http.Request, *Deps)
	}{
		{"missing key", func(_ *http.Request, d *Deps) { d.ShareReadRelayKey = nil }},
		{"wrong key", func(_ *http.Request, d *Deps) { d.ShareReadRelayKey = []byte(strings.Repeat("x", 32)) }},
		{"missing visitor", func(r *http.Request, _ *Deps) { r.Header.Del(shareVisitorHeader) }},
		{"missing timestamp", func(r *http.Request, _ *Deps) { r.Header.Del(shareTimestampHeader) }},
		{"missing signature", func(r *http.Request, _ *Deps) { r.Header.Del(shareSignatureHeader) }},
		{"empty signature", func(r *http.Request, _ *Deps) { r.Header.Set(shareSignatureHeader, "") }},
		{"duplicate visitor", func(r *http.Request, _ *Deps) { r.Header.Add(shareVisitorHeader, "198.51.100.8") }},
		{"duplicate timestamp", func(r *http.Request, _ *Deps) { r.Header.Add(shareTimestampHeader, "1792244800") }},
		{"duplicate signature", func(r *http.Request, _ *Deps) { r.Header.Add(shareSignatureHeader, r.Header.Get(shareSignatureHeader)) }},
		{"changed visitor", func(r *http.Request, _ *Deps) { r.Header.Set(shareVisitorHeader, "198.51.100.8") }},
		{"changed token", func(r *http.Request, _ *Deps) { r.URL.Path = "/shares/another-token" }},
		{"changed method", func(r *http.Request, _ *Deps) { r.Method = http.MethodPost }},
		{"other endpoint", func(r *http.Request, _ *Deps) {
			r.URL.Path = "/workflows/contract-token"
			signShareRelayRequest(r, "198.51.100.7", now.Unix(), key)
		}},
		{"unexpected query", func(r *http.Request, _ *Deps) { r.URL.RawQuery = "visitor=other" }},
		{"stale", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "198.51.100.7", now.Unix()-61, key) }},
		{"future", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "198.51.100.7", now.Unix()+6, key) }},
		{"nonnumeric timestamp", func(r *http.Request, _ *Deps) { r.Header.Set(shareTimestampHeader, "tomorrow") }},
		{"noncanonical timestamp", func(r *http.Request, _ *Deps) { r.Header.Set(shareTimestampHeader, "+1792244800") }},
		{"bad signature", func(r *http.Request, _ *Deps) { r.Header.Set(shareSignatureHeader, strings.Repeat("0", 64)) }},
		{"nonhex signature", func(r *http.Request, _ *Deps) { r.Header.Set(shareSignatureHeader, strings.Repeat("z", 64)) }},
		{"short signature", func(r *http.Request, _ *Deps) { r.Header.Set(shareSignatureHeader, "ab") }},
		{"invalid address", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "not-an-address", now.Unix(), key) }},
		{"address chain", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "198.51.100.7, 203.0.113.1", now.Unix(), key) }},
		{"noncanonical ipv6", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "2001:0db8::1", now.Unix(), key) }},
		{"scoped address", func(r *http.Request, _ *Deps) { signShareRelayRequest(r, "fe80::1%eth0", now.Unix(), key) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{ShareReadRelayKey: key}
			r := httptest.NewRequest(http.MethodGet, "/shares/contract-token", nil)
			signShareRelayRequest(r, "198.51.100.7", now.Unix(), key)
			tt.change(r, d)
			if _, err := d.shareReadClientIP(r, now); err == nil {
				t.Fatal("invalid relay accepted")
			}
			// No Store is configured: rejection must precede any share lookup.
			w := httptest.NewRecorder()
			d.GetShare(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("HTTP status %d, want 401", w.Code)
			}
		})
	}
	for _, visitor := range []string{"198.51.100.7", "2001:db8::7"} {
		for _, offset := range []int64{-60, 0, 5} {
			r := httptest.NewRequest(http.MethodGet, "/shares/contract-token", nil)
			signShareRelayRequest(r, visitor, now.Unix()+offset, key)
			got, err := (&Deps{ShareReadRelayKey: key}).shareReadClientIP(r, now)
			if err != nil || got != visitor {
				t.Fatalf("valid relay %q at %+d: %q, %v", visitor, offset, got, err)
			}
		}
	}
}

func TestShareRelayDoesNotChangeDirectPublicIdentity(t *testing.T) {
	for _, configured := range []bool{false, true} {
		d := &Deps{}
		if configured {
			d.ShareReadRelayKey = []byte(strings.Repeat("x", 32))
		}
		r := httptest.NewRequest(http.MethodGet, "/shares/direct-token", nil)
		r.RemoteAddr = "203.0.113.10:4123"
		got, err := d.shareReadClientIP(r, time.Now())
		if err != nil || got != "203.0.113.10" {
			t.Fatalf("direct socket address: %q, %v", got, err)
		}
		r.Header.Set("X-Forwarded-For", "198.51.100.19, 203.0.113.10")
		got, err = d.shareReadClientIP(r, time.Now())
		if err != nil || got != "198.51.100.19" {
			t.Fatalf("existing direct proxy semantics: %q, %v", got, err)
		}
		r.Header.Set(shareVisitorHeader, "198.51.100.99")
		if _, err := d.shareReadClientIP(r, time.Now()); err == nil {
			t.Fatal("unsigned relay address accepted")
		}
	}
}
