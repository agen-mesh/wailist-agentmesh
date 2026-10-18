package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	shareVisitorHeader   = "X-AgentMesh-Share-Visitor"
	shareTimestampHeader = "X-AgentMesh-Share-Timestamp"
	shareSignatureHeader = "X-AgentMesh-Share-Signature"
)

var errShareRelay = errors.New("invalid share relay")

// ParseShareReadRelayKey accepts a dedicated 32-byte hex key, or an unset relay.
func ParseShareReadRelayKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != sha256.Size {
		return nil, errors.New("SHARE_READ_RELAY_KEY must be 64 hexadecimal characters")
	}
	return key, nil
}

func (d *Deps) shareReadClientIP(r *http.Request, now time.Time) (string, error) {
	names := []string{shareVisitorHeader, shareTimestampHeader, shareSignatureHeader}
	relayed := false
	for _, name := range names {
		if len(r.Header.Values(name)) != 0 {
			relayed = true
		}
	}
	if !relayed {
		return clientIP(r), nil
	}
	if len(d.ShareReadRelayKey) != sha256.Size || r.Method != http.MethodGet ||
		r.URL.RawQuery != "" || !strings.HasPrefix(r.URL.Path, "/shares/") ||
		strings.TrimPrefix(r.URL.Path, "/shares/") == "" ||
		strings.Contains(strings.TrimPrefix(r.URL.Path, "/shares/"), "/") {
		return "", errShareRelay
	}
	for _, name := range names {
		if len(r.Header.Values(name)) != 1 || r.Header.Get(name) == "" {
			return "", errShareRelay
		}
	}

	visitor := r.Header.Get(shareVisitorHeader)
	addr, err := netip.ParseAddr(visitor)
	if err != nil || addr.Zone() != "" || addr.Unmap().String() != visitor {
		return "", errShareRelay
	}
	timestamp := r.Header.Get(shareTimestampHeader)
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp {
		return "", errShareRelay
	}
	current := now.Unix()
	if seconds < current-60 || seconds > current+5 {
		return "", errShareRelay
	}

	supplied, err := hex.DecodeString(r.Header.Get(shareSignatureHeader))
	if err != nil || len(supplied) != sha256.Size {
		return "", errShareRelay
	}
	payload := strings.Join([]string{
		"v1", r.Method, r.URL.EscapedPath(), visitor, timestamp,
	}, "\n")
	mac := hmac.New(sha256.New, d.ShareReadRelayKey)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		return "", errShareRelay
	}
	return visitor, nil
}
