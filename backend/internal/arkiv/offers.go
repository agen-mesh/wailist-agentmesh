package arkiv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ChainID         uint64 = 7738577
	DefaultRPCURL          = "https://rpc.tiramisu.db-chain.testnet.arkiv.network"
	SettlementChain uint64 = 5042002
	SettlementAsset        = "0x3600000000000000000000000000000000000000"
	Project                = "agentmesh-mumbai"
	OfferKind              = "service-offer-v1"
	DiscoveryQuery         = "project = str('agentmesh-mumbai') AND kind = str('service-offer-v1') AND settlement_chain = u64(5042002) AND status = str('active')"
	maxPayloadBytes        = 16 * 1024
	maxUnixTime     uint64 = 253402300799
)

var (
	ErrUnconfigured = errors.New("arkiv is not configured")
	ErrInvalidOffer = errors.New("invalid arkiv offer")
	ErrExpired      = errors.New("arkiv offer expired")
	ErrInactive     = errors.New("arkiv offer is inactive")
	ErrNotFound     = errors.New("arkiv offer not found or no longer live")
	ErrChanged      = errors.New("arkiv offer changed; a new approval is required")
	ErrEndpoint     = errors.New("arkiv offer endpoint is not allowed")
	ErrWrongChain   = errors.New("unexpected arkiv network")
	ErrRPC          = errors.New("arkiv RPC unavailable or malformed")
	capabilityRE    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	fieldRE         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	quantityRE      = regexp.MustCompile(`^0x(?:0|[1-9a-fA-F][0-9a-fA-F]{0,15})$`)
)

// EndpointPolicy must use the same destination rules as the merchant transport.
// That transport must also enforce DNS and redirect rules when it actually dials.
type EndpointPolicy func(context.Context, string) error

type InputField struct {
	Type string `json:"type"`
}

// Only structural schema fields are public. Defaults, examples and remote refs
// are deliberately unsupported so a form cannot silently publish input values.
type InputSchema struct {
	Type                 string                `json:"type"`
	Properties           map[string]InputField `json:"properties"`
	Required             []string              `json:"required"`
	AdditionalProperties *bool                 `json:"additionalProperties"`
}

type OfferPayload struct {
	Endpoint    string      `json:"endpoint"`
	Method      string      `json:"method"`
	Payee       string      `json:"payee"`
	InputSchema InputSchema `json:"input_schema"`
}

// Persist the complete server-read snapshot with the order. Browser-supplied
// snapshots are not evidence. Revalidate before approval and before signing.
type OfferSnapshot struct {
	EntityKey       string       `json:"entity_key"`
	Owner           string       `json:"owner"`
	Version         uint64       `json:"version,string"`
	ContentHash     string       `json:"content_hash"`
	SnapshotHash    string       `json:"snapshot_hash"`
	Capability      string       `json:"capability"`
	SettlementChain uint64       `json:"settlement_chain"`
	Asset           string       `json:"asset"`
	PriceMicros     uint64       `json:"price_micros,string"`
	Status          string       `json:"status"`
	ValidUntil      uint64       `json:"valid_until,string"`
	ExpiresAtBlock  uint64       `json:"expires_at_block,string"`
	UpdatedAtBlock  uint64       `json:"updated_at_block,string"`
	ObservedAtBlock uint64       `json:"observed_at_block,string"`
	Payload         OfferPayload `json:"payload"`
}

type rpcAttribute struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type rpcEntity struct {
	Key         string         `json:"key"`
	Owner       string         `json:"owner"`
	UpdatedAt   string         `json:"updatedAt"`
	ExpiresAt   string         `json:"expiresAt"`
	ContentType string         `json:"contentType"`
	Payload     string         `json:"payload"`
	Attributes  []rpcAttribute `json:"attributes"`
}

func parseOffer(ctx context.Context, raw json.RawMessage, head uint64, now time.Time, policy EndpointPolicy) (OfferSnapshot, error) {
	var entity rpcEntity
	if err := decodeJSON(raw, &entity, false); err != nil {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	if !hexBytes(entity.Key, 32) || !address(entity.Owner) || entity.ContentType != "application/json" {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	expires, err := quantity(entity.ExpiresAt)
	if err != nil {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	updated, err := quantity(entity.UpdatedAt)
	if err != nil || updated > head || updated >= expires {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	if expires <= head {
		return OfferSnapshot{}, ErrExpired
	}
	if !strings.HasPrefix(entity.Payload, "0x") || len(entity.Payload) > 2+2*maxPayloadBytes {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	payload, err := hex.DecodeString(entity.Payload[2:])
	if err != nil || !utf8.Valid(payload) {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	var body OfferPayload
	if err := decodeJSON(payload, &body, true); err != nil || !validSchema(body.InputSchema) {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	if body.Method != "POST" || !address(body.Payee) || !strings.EqualFold(body.Payee, entity.Owner) {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	if err := httpsURLSyntax(body.Endpoint); err != nil {
		return OfferSnapshot{}, ErrEndpoint
	}
	attrs := make(map[string]rpcAttribute, len(entity.Attributes))
	for _, attr := range entity.Attributes {
		if _, duplicate := attrs[attr.Name]; duplicate {
			return OfferSnapshot{}, ErrInvalidOffer
		}
		attrs[attr.Name] = attr
	}
	if len(attrs) != 9 {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	str := func(name, kind string) string {
		a, ok := attrs[name]
		var value string
		if !ok || a.Type != kind || json.Unmarshal(a.Value, &value) != nil {
			return ""
		}
		return value
	}
	u64 := func(name string) (uint64, error) { return quantity(str(name, "u64")) }
	if str("project", "str") != Project || str("kind", "str") != OfferKind {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	capability := str("capability", "str")
	if !capabilityRE.MatchString(capability) || !strings.EqualFold(str("asset", "addr"), SettlementAsset) {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	chain, chainErr := u64("settlement_chain")
	price, priceErr := u64("price_micros")
	version, versionErr := u64("version")
	until, untilErr := u64("valid_until")
	if chainErr != nil || chain != SettlementChain || priceErr != nil || price == 0 || versionErr != nil || version == 0 || untilErr != nil || until > maxUnixTime {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	status := str("status", "str")
	if status != "active" {
		return OfferSnapshot{}, ErrInactive
	}
	if now.Unix() < 0 || until <= uint64(now.Unix()) {
		return OfferSnapshot{}, ErrExpired
	}
	if policy == nil {
		return OfferSnapshot{}, ErrUnconfigured
	}
	if err := policy(ctx, body.Endpoint); err != nil {
		if ctx.Err() != nil {
			return OfferSnapshot{}, ctx.Err()
		}
		// Policy errors can contain resolved addresses or credential-bearing URLs.
		return OfferSnapshot{}, ErrEndpoint
	}
	body.Payee = strings.ToLower(body.Payee)
	hash := sha256.Sum256(payload)
	snapshot := OfferSnapshot{
		EntityKey: strings.ToLower(entity.Key), Owner: strings.ToLower(entity.Owner),
		Version: version, ContentHash: "0x" + hex.EncodeToString(hash[:]),
		Capability: capability, SettlementChain: chain, Asset: SettlementAsset,
		PriceMicros: price, Status: status, ValidUntil: until,
		ExpiresAtBlock: expires, UpdatedAtBlock: updated, ObservedAtBlock: head, Payload: body,
	}
	snapshot.SnapshotHash, err = snapshotDigest(snapshot)
	if err != nil {
		return OfferSnapshot{}, err
	}
	return snapshot, nil
}

func snapshotDigest(snapshot OfferSnapshot) (string, error) {
	// Observation height changes on reread; the public terms must not.
	snapshot.ObservedAtBlock = 0
	snapshot.SnapshotHash = ""
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode arkiv snapshot: %w", err)
	}
	hash := sha256.Sum256(append([]byte("agentmesh-arkiv-snapshot-v1\n"), encoded...))
	return "0x" + hex.EncodeToString(hash[:]), nil
}

func validSchema(s InputSchema) bool {
	if s.Type != "object" || s.Properties == nil || len(s.Properties) > 32 || s.Required == nil || s.AdditionalProperties == nil || *s.AdditionalProperties {
		return false
	}
	for name, field := range s.Properties {
		if !fieldRE.MatchString(name) || name == "constructor" || name == "prototype" {
			return false
		}
		switch field.Type {
		case "string", "integer", "number", "boolean":
		default:
			return false
		}
	}
	seen := map[string]bool{}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func httpsURLSyntax(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return ErrEndpoint
	}
	return nil
}

func hexBytes(s string, n int) bool {
	if len(s) != 2+n*2 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

func address(s string) bool {
	return hexBytes(s, 20) && s != "0x0000000000000000000000000000000000000000"
}

func quantity(s string) (uint64, error) {
	if !quantityRE.MatchString(s) {
		return 0, ErrInvalidOffer
	}
	return strconv.ParseUint(s[2:], 16, 64)
}

func decodeJSON(data []byte, target any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := uniqueValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	if strict {
		d.DisallowUnknownFields()
	}
	return d.Decode(target)
}

func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 12 {
		return errors.New("JSON nesting limit exceeded")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate JSON member")
			}
			seen[name] = true
		}
		if err := uniqueValue(d, depth+1); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return fmt.Errorf("invalid JSON container: %w", ErrInvalidOffer)
	}
	return nil
}
