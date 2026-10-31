package arkiv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(t *testing.T, rows func() []json.RawMessage, inspect func(string, []json.RawMessage)) *Client {
	t.Helper()
	c, err := NewClient(DefaultRPCURL, fixturePolicy)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Unix(1900000000, 0) }
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("invalid RPC request", r.Method)
		}
		var request struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      uint64            `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.JSONRPC != "2.0" || request.ID == 0 {
			t.Fatalf("invalid JSON-RPC request: %v", err)
		}
		if inspect != nil {
			inspect(request.Method, request.Params)
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0x76edaf"
		case "eth_blockNumber":
			result = "0x1000"
		case "arkiv_query":
			result = map[string]any{"data": rows(), "blockNumber": "0x1000"}
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	return c
}

func TestQueryUsesTypedPredicateAndLatestOffsetProjection(t *testing.T) {
	queryCalls := 0
	c := fixtureClient(t, func() []json.RawMessage { return []json.RawMessage{fixtureEntity()} }, func(method string, params []json.RawMessage) {
		if method != "arkiv_query" {
			return
		}
		queryCalls++
		var expression string
		var options struct {
			Limit  string          `json:"limit"`
			Offset string          `json:"offset"`
			Select map[string]bool `json:"select"`
		}
		if len(params) != 2 || json.Unmarshal(params[0], &expression) != nil || json.Unmarshal(params[1], &options) != nil {
			t.Fatal("invalid query arguments")
		}
		if expression != DiscoveryQuery+" AND capability = str('summarize') AND price_micros <= u64(1000000) ORDER BY $key ASC" || options.Limit != "0xa" || options.Offset != "0x14" {
			t.Fatalf("wrong query or context: %s %+v", expression, options)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(params[1], &fields) != nil || len(fields) != 3 || len(options.Select) != 5 {
			t.Fatal("unsupported Rabdi query option or projection")
		}
		for _, key := range []string{"key", "owner", "updatedAt", "expiresAt", "attributes"} {
			if !options.Select[key] {
				t.Fatalf("missing projection %s", key)
			}
		}
	})
	page, err := c.Query(context.Background(), QueryOptions{Capability: "summarize", MaxPriceMicros: 1000000, Limit: 10, Offset: 20})
	if err != nil || queryCalls != 1 || len(page.Offers) != 1 || page.BlockNumber != 4096 || page.Offset != 20 || page.Rejected != 0 {
		t.Fatalf("query: %+v %v", page, err)
	}
}

func TestQueryRejectsInvalidRowsAndIndependentlyChecksFilters(t *testing.T) {
	c := fixtureClient(t, func() []json.RawMessage {
		return []json.RawMessage{fixtureEntity(), editedEntity(t, func(e map[string]any) { e["owner"] = "bad" })}
	}, nil)
	page, err := c.Query(context.Background(), QueryOptions{})
	if err != nil || len(page.Offers) != 1 || page.Rejected != 1 {
		t.Fatalf("malformed row was not excluded: %+v %v", page, err)
	}
	for _, options := range []QueryOptions{{Capability: "other"}, {MaxPriceMicros: 1}} {
		page, err := c.Query(context.Background(), options)
		if err != nil || len(page.Offers) != 0 || page.Rejected != 2 {
			t.Fatalf("RPC predicate trusted: %+v %v", page, err)
		}
	}
	for _, options := range []QueryOptions{{Capability: "x') OR true"}, {Limit: 51}} {
		if _, err := c.Query(context.Background(), options); !errors.Is(err, ErrInvalidOffer) {
			t.Fatalf("invalid query accepted: %+v %v", options, err)
		}
	}
}

func TestGetAndRevalidationRejectChangedTerms(t *testing.T) {
	current := fixtureEntity()
	c := fixtureClient(t, func() []json.RawMessage { return []json.RawMessage{current} }, nil)
	stored, err := c.Get(context.Background(), fixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Revalidate(context.Background(), stored); err != nil {
		t.Fatal("unchanged offer rejected", err)
	}
	tests := []struct {
		name string
		edit func(map[string]any)
		want error
	}{
		{"price without version bump", func(e map[string]any) { setAttribute(e, "price_micros", "u64", "0xf4241") }, ErrChanged},
		{"version", func(e map[string]any) { setAttribute(e, "version", "u64", "0x2") }, ErrChanged},
		{"payload without version bump", func(e map[string]any) { setPayload(e, fixturePayload+" ") }, ErrChanged},
		{"owner and payee transfer", func(e map[string]any) {
			e["owner"] = "0x3333333333333333333333333333333333333333"
			setPayload(e, strings.ReplaceAll(fixturePayload, fixtureOwner, e["owner"].(string)))
		}, ErrChanged},
		{"owner transfer without payee", func(e map[string]any) { e["owner"] = "0x3333333333333333333333333333333333333333" }, ErrInvalidOffer},
		{"native expiry", func(e map[string]any) { e["expiresAt"] = "0x1000" }, ErrExpired},
		{"native extension", func(e map[string]any) { e["expiresAt"] = "0x1201" }, ErrChanged},
		{"status", func(e map[string]any) { setAttribute(e, "status", "str", "paused") }, ErrInactive},
		{"updated block", func(e map[string]any) { e["updatedAt"] = "0xff1" }, ErrChanged},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			current = editedEntity(t, tc.edit)
			if _, err := c.Revalidate(context.Background(), stored); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	current = fixtureEntity()
	stored.PriceMicros++
	if _, err := c.Revalidate(context.Background(), stored); !errors.Is(err, ErrInvalidOffer) {
		t.Fatalf("modified stored snapshot accepted: %v", err)
	}
}

func TestRPCFailuresAndMissingOffersFailClosed(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"secret"}}`,
		`{"jsonrpc":"2.0","id":1,"error":null,"result":"0x7614d1"}`,
		`{"jsonrpc":"1.0","id":1,"result":"0x7614d1"}`,
		`{"jsonrpc":"2.0","id":999,"result":"0x7614d1"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x7614d1","result":"0x1"}`,
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1}`,
		`not-json`,
		strings.Repeat(" ", maxResponseBytes+1),
	} {
		c := fixtureClient(t, nil, nil)
		c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		if _, err := c.Get(context.Background(), fixtureKey); !errors.Is(err, ErrRPC) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("bad RPC response accepted or leaked: %v", err)
		}
	}
	for _, status := range []int{301, 401, 429, 500} {
		c := fixtureClient(t, nil, nil)
		c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret"))}, nil
		})
		if _, err := c.Get(context.Background(), fixtureKey); !errors.Is(err, ErrRPC) {
			t.Fatalf("HTTP %d accepted: %v", status, err)
		}
	}
	c := fixtureClient(t, nil, nil)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret URL") })
	if _, err := c.Get(context.Background(), fixtureKey); !errors.Is(err, ErrRPC) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("transport error not contained: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, fixtureKey); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	c = fixtureClient(t, func() []json.RawMessage { return []json.RawMessage{} }, nil)
	if _, err := c.Get(context.Background(), fixtureKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entity accepted: %v", err)
	}
}

func TestRPCRejectsWrongNetworkAndQueryShape(t *testing.T) {
	for _, result := range []string{`"0x1"`, `"0x7614d1"`, `{"data":null,"blockNumber":"0x1000"}`, `{"data":[],"blockNumber":"0x1001"}`, `{"data":[],"blockNumber":"0xfff"}`, `{"data":[],"blockNumber":4096}`} {
		c := fixtureClient(t, nil, nil)
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var request struct {
				ID     uint64
				Method string
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			value := result
			if strings.HasPrefix(result, "{") && request.Method == "eth_chainId" {
				value = `"0x76edaf"`
			} else if request.Method == "eth_blockNumber" {
				value = `"0x1000"`
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, request.ID, value)))}, nil
		})
		_, err := c.Query(context.Background(), QueryOptions{})
		if err == nil {
			t.Fatalf("malformed result accepted: %s", result)
		}
	}
}

func TestRevalidateRejectsLegacyAndCrossNetworkSnapshotsBeforeRPC(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func() []json.RawMessage { return []json.RawMessage{fixtureEntity()} }, func(string, []json.RawMessage) { calls++ })
	current, err := c.Get(context.Background(), fixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, chain := range []uint64{0, 7738577, 1} {
		stored := current
		stored.ArkivChainID = chain
		stored.SnapshotHash, err = snapshotDigest(stored)
		if err != nil || stored.SnapshotHash == current.SnapshotHash {
			t.Fatal("network missing from snapshot digest", err)
		}
		encoded, _ := json.Marshal(stored)
		before := calls
		if _, err := c.Revalidate(context.Background(), stored); !errors.Is(err, ErrSnapshotNetwork) || calls != before {
			t.Fatalf("network %d reused matching entity key or called RPC: %v", chain, err)
		}
		after, _ := json.Marshal(stored)
		if string(encoded) != string(after) {
			t.Fatal("historical snapshot mutated")
		}
	}
	// Preserve the original v1 serialization, which had no network field.
	legacy := current
	legacy.ObservedAtBlock, legacy.SnapshotHash = 0, ""
	encoded, _ := json.Marshal(legacy)
	encoded = []byte(strings.Replace(string(encoded), `"arkiv_chain_id":7794095,`, "", 1))
	oldHash := sha256.Sum256(append([]byte("agentmesh-arkiv-snapshot-v1\n"), encoded...))
	var restored OfferSnapshot
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	restored.SnapshotHash = "0x" + hex.EncodeToString(oldHash[:])
	before := calls
	if _, err := c.Revalidate(context.Background(), restored); !errors.Is(err, ErrSnapshotNetwork) || calls != before {
		t.Fatal("legacy unbound snapshot accepted", err)
	}
	restored.ArkivChainID = ChainID
	if _, err := c.Revalidate(context.Background(), restored); !errors.Is(err, ErrInvalidOffer) || calls != before {
		t.Fatal("adding network reinterpreted old approval", err)
	}
}

func TestLatestQueryChecksResponseBlockAndExpiryAtFreshHead(t *testing.T) {
	for _, tc := range []struct {
		name         string
		query, after string
		chainAfter   string
		want         error
	}{
		{"advancing head", "0x1001", "0x1002", "0x76edaf", nil},
		{"stale response", "0xfff", "0x1002", "0x76edaf", ErrRPC},
		{"future response", "0x1003", "0x1002", "0x76edaf", ErrRPC},
		{"expired during read", "0x11ff", "0x1200", "0x76edaf", ErrExpired},
		{"network changed during read", "0x1001", "0x1002", "0x7614d1", ErrWrongChain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, nil, nil)
			queried := false
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var request struct {
					ID     uint64
					Method string
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				var result any
				switch request.Method {
				case "eth_chainId":
					result = "0x76edaf"
					if queried {
						result = tc.chainAfter
					}
				case "eth_blockNumber":
					result = "0x1000"
					if queried {
						result = tc.after
					}
				case "arkiv_query":
					queried = true
					result = map[string]any{"data": []json.RawMessage{fixtureEntity()}, "blockNumber": tc.query}
				default:
					t.Fatalf("unexpected RPC %s", request.Method)
				}
				body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			snapshot, err := c.Get(context.Background(), fixtureKey)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err == nil && snapshot.ObservedAtBlock != 4098 {
				t.Fatal("did not use fresh head")
			}
		})
	}
}

func TestUnconfiguredClientAndRedirects(t *testing.T) {
	for _, rpc := range []string{"", "http://rpc.example", "https://user:secret@rpc.example", "https://rpc.example?key=secret"} {
		if _, err := NewClient(rpc, fixturePolicy); err == nil {
			t.Fatalf("invalid configuration accepted: %s", rpc)
		}
	}
	if _, err := NewClient(DefaultRPCURL, nil); !errors.Is(err, ErrUnconfigured) {
		t.Fatal(err)
	}
	var absent *Client
	if _, err := absent.Query(context.Background(), QueryOptions{}); !errors.Is(err, ErrUnconfigured) {
		t.Fatal(err)
	}
	c := fixtureClient(t, nil, nil)
	if err := c.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirects allowed: %v", err)
	}
}
