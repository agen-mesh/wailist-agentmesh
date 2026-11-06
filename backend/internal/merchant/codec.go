package merchant

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const quoteDomain = "agentmesh-merchant-quote-v1\n"

func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(h[:])
}

func quoteMessage(q Quote) ([]byte, error) {
	q.Proof = ""
	b, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("%w: quote encoding", ErrInvalid)
	}
	return append([]byte(quoteDomain), b...), nil
}

func QuoteDigest(q Quote) (string, error) {
	b, err := json.Marshal(q)
	if err != nil {
		return "", fmt.Errorf("%w: quote encoding", ErrInvalid)
	}
	return Digest(b), nil
}

// VerifyQuote validates the original signed terms without imposing today's expiry.
func VerifyQuote(q Quote, public ed25519.PublicKey) error {
	if len(public) != ed25519.PublicKeySize || !validHex(q.ID, 32) ||
		q.Method != http.MethodPost || !validResource(q.Resource) ||
		!validHex(q.RequestDigest, 32) || q.ChainID != ChainID || q.Asset != Asset ||
		!validAddress(q.Payee) || !validAmount(q.AmountMicros) ||
		!wholeUTC(q.Deadline) || !validHex(q.Proof, ed25519.SignatureSize) {
		return fmt.Errorf("%w: quote terms", ErrInvalid)
	}
	sig, _ := hex.DecodeString(q.Proof[2:])
	message, err := quoteMessage(q)
	if err != nil {
		return err
	}
	if !ed25519.Verify(public, message, sig) {
		return fmt.Errorf("%w: quote signature", ErrInvalid)
	}
	return nil
}

// DecodeIntent rejects alternate encodings so the stored bytes remain authoritative.
func DecodeIntent(raw json.RawMessage) (Intent, error) {
	var i Intent
	if err := strictJSON(raw, &i, 16384); err != nil {
		return i, err
	}
	b, err := json.Marshal(i)
	if err != nil || !bytes.Equal(b, raw) {
		return i, fmt.Errorf("%w: noncanonical intent", ErrInvalid)
	}
	if i.Version != Protocol || !validID(i.OrderID) || !validID(i.AccountID) ||
		!optionalID(i.WorkflowID) || !optionalID(i.RunID) || !optionalID(i.NodeID) || !optionalID(i.Invocation) ||
		!boundedString(i.OfferKey, 512) || !boundedString(i.OfferVersion, 128) ||
		!validAddress(i.OfferOwner) || i.OfferOwner != i.Payee ||
		!validHex(i.OfferContentHash, 32) || !validHex(i.OfferSnapshotHash, 32) ||
		!validHex(i.OrderSalt, 32) || !validAddress(i.Payer) || !validAddress(i.Payee) ||
		i.ChainID != ChainID || i.Asset != Asset || !validAmount(i.AmountMicros) || !validAmount(i.MaxMicros) ||
		i.Method != http.MethodPost || !validResource(i.Resource) || !validHex(i.RequestDigest, 32) || !validHex(i.QuoteDigest, 32) {
		return i, fmt.Errorf("%w: intent terms", ErrInvalid)
	}
	price, _ := strconv.ParseInt(i.AmountMicros, 10, 64)
	limit, _ := strconv.ParseInt(i.MaxMicros, 10, 64)
	after, aerr := decimal(i.ValidAfter)
	before, berr := decimal(i.ValidBefore)
	if limit < price || aerr != nil || berr != nil || after >= before {
		return i, fmt.Errorf("%w: intent limits", ErrInvalid)
	}
	return i, nil
}

func ValidateSnapshot(s Snapshot, q Quote, public ed25519.PublicKey) error {
	if !validHex(s.IntentHash, 32) || Digest(s.CanonicalIntent) != s.IntentHash {
		return fmt.Errorf("%w: intent hash", ErrInvalid)
	}
	i, err := DecodeIntent(s.CanonicalIntent)
	if err != nil {
		return err
	}
	if _, err := ParseRequest(s.Request); err != nil {
		return err
	}
	if err := VerifyQuote(q, public); err != nil {
		return err
	}
	qd, err := QuoteDigest(q)
	if err != nil {
		return err
	}
	before, _ := decimal(i.ValidBefore)
	if i.OrderID != s.OrderID || i.Payer != s.Payer || i.Payee != s.Payee ||
		i.ChainID != s.ChainID || i.Asset != s.Asset || i.AmountMicros != s.AmountMicros ||
		i.Method != s.Method || i.Resource != s.Resource || i.RequestDigest != s.RequestDigest ||
		i.QuoteDigest != s.QuoteDigest || !wholeUTC(s.Deadline) || before != s.Deadline.Unix() ||
		Digest(s.Request) != s.RequestDigest || s.QuoteDigest != qd ||
		q.Method != s.Method || q.Resource != s.Resource || q.RequestDigest != s.RequestDigest ||
		q.ChainID != s.ChainID || q.Asset != s.Asset || q.Payee != s.Payee ||
		q.AmountMicros != s.AmountMicros || !q.Deadline.Equal(s.Deadline) {
		return fmt.Errorf("%w: snapshot does not match signed terms", ErrInvalid)
	}
	return nil
}

func ParseRequest(raw json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := strictJSON(raw, &fields, MaxRequestBytes); err != nil {
		return "", err
	}
	b, ok := fields["text"]
	if len(fields) != 1 || !ok || bytes.Equal(b, []byte("null")) {
		return "", fmt.Errorf("%w: expected text request", ErrInvalid)
	}
	var text string
	if err := json.Unmarshal(b, &text); err != nil || len(text) > MaxTextBytes || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: text bounds", ErrInvalid)
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", fmt.Errorf("%w: text control character", ErrInvalid)
		}
	}
	return text, nil
}

func strictJSON(raw []byte, dst any, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) || !json.Valid(raw) || !validSurrogates(raw) {
		return fmt.Errorf("%w: JSON bounds or encoding", ErrInvalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueJSON(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", ErrInvalid)
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("%w: JSON schema", ErrInvalid)
	}
	return nil
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 12 {
		return fmt.Errorf("%w: JSON depth", ErrInvalid)
	}
	t, err := d.Token()
	if err != nil {
		return fmt.Errorf("%w: JSON token", ErrInvalid)
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return fmt.Errorf("%w: JSON key", ErrInvalid)
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("%w: duplicate JSON key", ErrInvalid)
			}
			seen[name] = true
		}
		if err := uniqueJSON(d, depth+1); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return fmt.Errorf("%w: JSON close", ErrInvalid)
	}
	return nil
}

// encoding/json replaces lone surrogate escapes, which would lose exact input meaning.
func validSurrogates(raw []byte) bool {
	for p := 0; p < len(raw); p++ {
		if raw[p] != '\\' {
			continue
		}
		p++
		if p >= len(raw) || raw[p] != 'u' {
			continue
		}
		if p+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[p+1:p+5]), 16, 16)
		if err != nil {
			return false
		}
		p += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if p+6 >= len(raw) || raw[p+1] != '\\' || raw[p+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[p+3:p+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			p += 6
		}
	}
	return true
}

func validHex(s string, size int) bool {
	if len(s) != 2+size*2 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, c := range s[2:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func validAddress(s string) bool {
	return validHex(s, 20) && s != "0x"+strings.Repeat("0", 40)
}

func decimal(s string) (int64, error) {
	if s == "" || len(s) > 19 || (len(s) > 1 && s[0] == '0') {
		return 0, ErrInvalid
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

func validAmount(s string) bool {
	n, err := decimal(s)
	return err == nil && n > 0
}

func wholeUTC(t time.Time) bool {
	_, offset := t.Zone()
	return !t.IsZero() && t.Unix() > 0 && t.Nanosecond() == 0 && offset == 0
}

func validResource(s string) bool {
	u, err := url.Parse(s)
	return err == nil && len(s) <= 2048 && u.Scheme == "https" && u.Hostname() != "" &&
		u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == s
}

func boundedString(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func optionalID(s string) bool { return s == "" || validID(s) }
