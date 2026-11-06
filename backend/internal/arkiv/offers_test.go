package arkiv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const fixtureKey = "0x1111111111111111111111111111111111111111111111111111111111111111"
const fixtureOwner = "0x2222222222222222222222222222222222222222"
const fixturePayload = `{"endpoint":"https://merchant.example/tool","method":"POST","payee":"0x2222222222222222222222222222222222222222","input_schema":{"type":"object","properties":{"prompt":{"type":"string"}},"required":["prompt"],"additionalProperties":false}}`

// Independently authored wire fixture follows the documented JSON-RPC shape.
// It is not a live response and is not encoded by the production writer.
func fixtureEntity() json.RawMessage {
	return json.RawMessage(strings.ReplaceAll(`{
		"key":"0x1111111111111111111111111111111111111111111111111111111111111111",
		"owner":"0x2222222222222222222222222222222222222222",
		"createdAt":"0xf00","updatedAt":"0xff0","expiresAt":"0x1200",
		"attributes":[
			{"name":"arkiv.schema","type":"str","value":"agentmesh.service-offer.v2"},
			{"name":"content_type","type":"str","value":"application/json"},
			{"name":"payload","type":"bytes","value":"PAYLOAD_HEX"},
			{"name":"project","type":"str","value":"agentmesh-mumbai"},
			{"name":"kind","type":"str","value":"service-offer-v2"},
			{"name":"capability","type":"str","value":"summarize"},
			{"name":"settlement_chain","type":"u64","value":"0x4cef52"},
			{"name":"asset","type":"addr","value":"0x3600000000000000000000000000000000000000"},
			{"name":"price_micros","type":"u64","value":"0xf4240"},
			{"name":"version","type":"u64","value":"0x1"},
			{"name":"status","type":"str","value":"active"},
			{"name":"valid_until","type":"u64","value":"0x77359400"}
		]
	}`, "PAYLOAD_HEX", "0x"+hex.EncodeToString([]byte(fixturePayload))))
}

func editedEntity(t *testing.T, edit func(map[string]any)) json.RawMessage {
	t.Helper()
	var entity map[string]any
	if err := json.Unmarshal(fixtureEntity(), &entity); err != nil {
		t.Fatal(err)
	}
	edit(entity)
	raw, err := json.Marshal(entity)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func setAttribute(entity map[string]any, name, kind string, value any) {
	for _, raw := range entity["attributes"].([]any) {
		a := raw.(map[string]any)
		if a["name"] == name {
			a["type"], a["value"] = kind, value
			return
		}
	}
	panic("fixture attribute not found")
}

func setPayload(entity map[string]any, raw string) {
	setAttribute(entity, "payload", "bytes", "0x"+hex.EncodeToString([]byte(raw)))
}

func fixturePolicy(_ context.Context, endpoint string) error {
	if endpoint != "https://merchant.example/tool" {
		return errors.New("fixture policy rejects destination")
	}
	return nil
}

func TestOfferWireFixtureAndExactIntegerUnits(t *testing.T) {
	snapshot, err := parseOffer(context.Background(), fixtureEntity(), 4096, time.Unix(1900000000, 0), fixturePolicy)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(fixturePayload))
	if snapshot.ArkivChainID != 7794095 || snapshot.EntityKey != fixtureKey || snapshot.Owner != fixtureOwner || snapshot.Payload.Payee != snapshot.Owner || snapshot.PriceMicros != 1000000 || snapshot.SettlementChain != 5042002 || snapshot.Asset != SettlementAsset || snapshot.ExpiresAtBlock != 4608 || snapshot.ValidUntil != 2000000000 || snapshot.ContentHash != "0x"+hex.EncodeToString(hash[:]) {
		t.Fatalf("unexpected parsed snapshot: %+v", snapshot)
	}
	raw := editedEntity(t, func(e map[string]any) { setAttribute(e, "price_micros", "u64", "0xffffffffffffffff") })
	max, err := parseOffer(context.Background(), raw, 4096, time.Unix(1900000000, 0), fixturePolicy)
	if err != nil || max.PriceMicros != ^uint64(0) {
		t.Fatalf("u64 amount changed: %d %v", max.PriceMicros, err)
	}
	encoded, err := json.Marshal(max)
	if err != nil || !strings.Contains(string(encoded), `"price_micros":"18446744073709551615"`) {
		t.Fatalf("amount not preserved as a decimal string: %s %v", encoded, err)
	}
}

func TestOfferRejectsMalformedAndUnsafeRecords(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want error
	}{
		{"key", func(e map[string]any) { e["key"] = "0x12" }, ErrInvalidOffer},
		{"owner", func(e map[string]any) { e["owner"] = "0x0000000000000000000000000000000000000000" }, ErrInvalidOffer},
		{"content type", func(e map[string]any) { setAttribute(e, "content_type", "str", "text/html") }, ErrInvalidOffer},
		{"invalid hex", func(e map[string]any) { setAttribute(e, "payload", "bytes", "0xzz") }, ErrInvalidOffer},
		{"invalid utf8", func(e map[string]any) { setAttribute(e, "payload", "bytes", "0xff") }, ErrInvalidOffer},
		{"wrong payload type", func(e map[string]any) { setAttribute(e, "payload", "str", "0x00") }, ErrInvalidOffer},
		{"huge payload", func(e map[string]any) {
			setAttribute(e, "payload", "bytes", "0x"+strings.Repeat("00", maxPayloadBytes+1))
		}, ErrInvalidOffer},
		{"legacy wire", func(e map[string]any) {
			e["attributes"] = e["attributes"].([]any)[3:]
			e["contentType"] = "application/json"
			e["payload"] = "0x" + hex.EncodeToString([]byte(fixturePayload))
		}, ErrInvalidOffer},
		{"wrong schema", func(e map[string]any) { setAttribute(e, "arkiv.schema", "str", "agentmesh.service-offer.v1") }, ErrInvalidOffer},
		{"native expiry equal", func(e map[string]any) { e["expiresAt"] = "0x1000" }, ErrExpired},
		{"native expiry earlier", func(e map[string]any) { e["expiresAt"] = "0xfff" }, ErrExpired},
		{"native expiry not timestamp", func(e map[string]any) { e["expiresAt"] = 2000000000 }, ErrInvalidOffer},
		{"native expiry missing", func(e map[string]any) { delete(e, "expiresAt") }, ErrInvalidOffer},
		{"future update", func(e map[string]any) { e["updatedAt"] = "0x1001" }, ErrInvalidOffer},
		{"UTC expiry equal", func(e map[string]any) { setAttribute(e, "valid_until", "u64", "0x713fb300") }, ErrExpired},
		{"UTC overflow", func(e map[string]any) { setAttribute(e, "valid_until", "u64", "0xffffffffffffffff") }, ErrInvalidOffer},
		{"inactive", func(e map[string]any) { setAttribute(e, "status", "str", "paused") }, ErrInactive},
		{"other project", func(e map[string]any) { setAttribute(e, "project", "str", "other") }, ErrInvalidOffer},
		{"other kind", func(e map[string]any) { setAttribute(e, "kind", "str", "receipt") }, ErrInvalidOffer},
		{"capability injection", func(e map[string]any) { setAttribute(e, "capability", "str", "x') OR true") }, ErrInvalidOffer},
		{"wrong chain", func(e map[string]any) { setAttribute(e, "settlement_chain", "u64", "0x1") }, ErrInvalidOffer},
		{"wrong token", func(e map[string]any) { setAttribute(e, "asset", "addr", fixtureOwner) }, ErrInvalidOffer},
		{"string token type", func(e map[string]any) { setAttribute(e, "asset", "str", SettlementAsset) }, ErrInvalidOffer},
		{"string amount", func(e map[string]any) { setAttribute(e, "price_micros", "str", "1000000") }, ErrInvalidOffer},
		{"JSON number amount", func(e map[string]any) { setAttribute(e, "price_micros", "u64", 1000000) }, ErrInvalidOffer},
		{"zero amount", func(e map[string]any) { setAttribute(e, "price_micros", "u64", "0x0") }, ErrInvalidOffer},
		{"overflow amount", func(e map[string]any) { setAttribute(e, "price_micros", "u64", "0x10000000000000000") }, ErrInvalidOffer},
		{"padded amount", func(e map[string]any) { setAttribute(e, "price_micros", "u64", "0x01") }, ErrInvalidOffer},
		{"zero version", func(e map[string]any) { setAttribute(e, "version", "u64", "0x0") }, ErrInvalidOffer},
		{"missing attribute", func(e map[string]any) { e["attributes"] = e["attributes"].([]any)[1:] }, ErrInvalidOffer},
		{"duplicate attribute", func(e map[string]any) { a := e["attributes"].([]any); a[1] = a[0] }, ErrInvalidOffer},
		{"extra public input", func(e map[string]any) {
			e["attributes"] = append(e["attributes"].([]any), map[string]any{"name": "request", "type": "str", "value": "private"})
		}, ErrInvalidOffer},
		{"wrong payee", func(e map[string]any) {
			setPayload(e, strings.ReplaceAll(fixturePayload, fixtureOwner, "0x3333333333333333333333333333333333333333"))
		}, ErrInvalidOffer},
		{"wrong method", func(e map[string]any) { setPayload(e, strings.ReplaceAll(fixturePayload, `"POST"`, `"GET"`)) }, ErrInvalidOffer},
		{"public private input", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `{"endpoint"`, `{"credentials":"secret","endpoint"`, 1))
		}, ErrInvalidOffer},
		{"schema default", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `"type":"string"`, `"type":"string","default":"private"`, 1))
		}, ErrInvalidOffer},
		{"schema external ref", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `"type":"string"`, `"$ref":"https://private.example"`, 1))
		}, ErrInvalidOffer},
		{"schema additional fields", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `"additionalProperties":false`, `"additionalProperties":true`, 1))
		}, ErrInvalidOffer},
		{"schema duplicate required", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `["prompt"]`, `["prompt","prompt"]`, 1))
		}, ErrInvalidOffer},
		{"schema missing required field", func(e map[string]any) { setPayload(e, strings.Replace(fixturePayload, `["prompt"]`, `["absent"]`, 1)) }, ErrInvalidOffer},
		{"duplicate payload field", func(e map[string]any) {
			setPayload(e, strings.Replace(fixturePayload, `{"endpoint"`, `{"method":"GET","endpoint"`, 1))
		}, ErrInvalidOffer},
		{"trailing JSON", func(e map[string]any) { setPayload(e, fixturePayload+`{}`) }, ErrInvalidOffer},
	}
	for _, endpoint := range []string{"http://merchant.example/tool", "https://user:pass@merchant.example/tool", "https://merchant.example/tool?secret=value", "https://merchant.example/tool#private", "https://127.0.0.1/tool", "https://[::1]/tool", "https://169.254.169.254/tool", "https://internal.example/tool"} {
		tests = append(tests, struct {
			name string
			edit func(map[string]any)
			want error
		}{endpoint, func(e map[string]any) {
			setPayload(e, strings.ReplaceAll(fixturePayload, "https://merchant.example/tool", endpoint))
		}, ErrEndpoint})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOffer(context.Background(), editedEntity(t, tc.edit), 4096, time.Unix(1900000000, 0), fixturePolicy)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSnapshotBindsTermsButNotObservationBlock(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1900000000, 0)
	first, err := parseOffer(ctx, fixtureEntity(), 4096, now, fixturePolicy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseOffer(ctx, fixtureEntity(), 4097, now, fixturePolicy)
	if err != nil || first.SnapshotHash != second.SnapshotHash {
		t.Fatal("observation changed the terms hash", err)
	}
	for _, attr := range []string{"price_micros", "version", "valid_until"} {
		raw := editedEntity(t, func(e map[string]any) { setAttribute(e, attr, "u64", "0x77359401") })
		changed, err := parseOffer(ctx, raw, 4096, now, fixturePolicy)
		if err != nil || changed.SnapshotHash == first.SnapshotHash {
			t.Fatalf("%s change not bound: %v", attr, err)
		}
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var restored OfferSnapshot
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	digest, err := snapshotDigest(restored)
	if err != nil || digest != first.SnapshotHash {
		t.Fatal("stored snapshot does not survive JSON round trip", err)
	}
}

func TestEndpointPolicyIsMandatoryAndSanitizesErrors(t *testing.T) {
	for _, policy := range []EndpointPolicy{nil, func(context.Context, string) error { return fmt.Errorf("sensitive-token") }} {
		_, err := parseOffer(context.Background(), fixtureEntity(), 4096, time.Unix(1900000000, 0), policy)
		if err == nil || strings.Contains(err.Error(), "sensitive-token") {
			t.Fatalf("endpoint policy failed open or leaked error: %v", err)
		}
	}
}
