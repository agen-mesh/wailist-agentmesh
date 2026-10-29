package arkiv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxResponseBytes = 2 * 1024 * 1024

type Client struct {
	rpcURL string
	policy EndpointPolicy
	http   *http.Client
	now    func() time.Time
	nextID atomic.Uint64
}

func NewClient(rpcURL string, policy EndpointPolicy) (*Client, error) {
	if rpcURL == "" || policy == nil {
		return nil, ErrUnconfigured
	}
	if httpsURLSyntax(rpcURL) != nil {
		return nil, fmt.Errorf("invalid arkiv RPC URL: %w", ErrEndpoint)
	}
	return &Client{
		rpcURL: rpcURL, policy: policy, now: time.Now,
		http: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

type QueryOptions struct {
	Capability     string
	MaxPriceMicros uint64
	Limit          uint64
	Cursor         string
	AtBlock        uint64
}

type OfferPage struct {
	Offers      []OfferSnapshot `json:"offers"`
	Rejected    int             `json:"rejected"`
	Cursor      string          `json:"cursor,omitempty"`
	BlockNumber uint64          `json:"block_number,string"`
	Query       string          `json:"query"`
}

func (c *Client) Query(ctx context.Context, options QueryOptions) (OfferPage, error) {
	expression := DiscoveryQuery
	if options.Capability != "" {
		if !capabilityRE.MatchString(options.Capability) {
			return OfferPage{}, ErrInvalidOffer
		}
		expression += " AND capability = str('" + options.Capability + "')"
	}
	if options.MaxPriceMicros > 0 {
		expression += " AND price_micros <= u64(" + strconv.FormatUint(options.MaxPriceMicros, 10) + ")"
	}
	head, err := c.head(ctx)
	if err != nil {
		return OfferPage{}, err
	}
	raw, at, cursor, err := c.query(ctx, expression, options, head)
	if err != nil {
		return OfferPage{}, err
	}
	page := OfferPage{Offers: []OfferSnapshot{}, BlockNumber: at, Cursor: cursor, Query: expression}
	seen := make(map[string]bool, len(raw))
	for _, entity := range raw {
		snapshot, err := parseOffer(ctx, entity, head, c.now(), c.policy)
		if err != nil {
			if ctx.Err() != nil {
				return OfferPage{}, ctx.Err()
			}
			page.Rejected++
			continue
		}
		if seen[snapshot.EntityKey] {
			return OfferPage{}, ErrRPC
		}
		seen[snapshot.EntityKey] = true
		// Independently enforce filters even if the RPC ignores its predicate.
		if snapshot.UpdatedAtBlock > at || (options.Capability != "" && snapshot.Capability != options.Capability) || (options.MaxPriceMicros > 0 && snapshot.PriceMicros > options.MaxPriceMicros) {
			page.Rejected++
			continue
		}
		page.Offers = append(page.Offers, snapshot)
	}
	return page, nil
}

func (c *Client) Get(ctx context.Context, entityKey string) (OfferSnapshot, error) {
	if !hexBytes(entityKey, 32) {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	head, err := c.head(ctx)
	if err != nil {
		return OfferSnapshot{}, err
	}
	// The SDK also implements getEntity through a key predicate. Pinning the
	// query to the observed block gives terms and native expiry one context.
	entities, _, _, err := c.query(ctx, "$key = key("+strings.ToLower(entityKey)+")", QueryOptions{Limit: 1}, head)
	if err != nil {
		return OfferSnapshot{}, err
	}
	if len(entities) == 0 {
		return OfferSnapshot{}, ErrNotFound
	}
	snapshot, err := parseOffer(ctx, entities[0], head, c.now(), c.policy)
	if err != nil {
		return OfferSnapshot{}, err
	}
	if !strings.EqualFold(snapshot.EntityKey, entityKey) {
		return OfferSnapshot{}, ErrRPC
	}
	return snapshot, nil
}

func (c *Client) Revalidate(ctx context.Context, stored OfferSnapshot) (OfferSnapshot, error) {
	digest, err := snapshotDigest(stored)
	if err != nil || !hexBytes(stored.SnapshotHash, 32) || digest != stored.SnapshotHash {
		return OfferSnapshot{}, ErrInvalidOffer
	}
	current, err := c.Get(ctx, stored.EntityKey)
	if err != nil {
		return OfferSnapshot{}, err
	}
	if current.SnapshotHash != stored.SnapshotHash {
		return OfferSnapshot{}, ErrChanged
	}
	return current, nil
}

func (c *Client) head(ctx context.Context) (uint64, error) {
	if c == nil || c.rpcURL == "" || c.policy == nil || c.http == nil || c.now == nil {
		return 0, ErrUnconfigured
	}
	var chainHex, headHex string
	if err := c.call(ctx, "eth_chainId", []any{}, &chainHex); err != nil {
		return 0, err
	}
	chain, err := quantity(chainHex)
	if err != nil || chain != ChainID {
		return 0, ErrWrongChain
	}
	if err := c.call(ctx, "eth_blockNumber", []any{}, &headHex); err != nil {
		return 0, err
	}
	head, err := quantity(headHex)
	if err != nil {
		return 0, ErrRPC
	}
	return head, nil
}

func (c *Client) query(ctx context.Context, expression string, options QueryOptions, head uint64) ([]json.RawMessage, uint64, string, error) {
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit > 50 || len(options.Cursor) > 2048 || (options.Cursor != "" && options.AtBlock == 0) {
		return nil, 0, "", ErrInvalidOffer
	}
	at := head
	if options.AtBlock != 0 {
		if options.AtBlock > head {
			return nil, 0, "", ErrInvalidOffer
		}
		at = options.AtBlock
	}
	projection := map[string]bool{"key": true, "owner": true, "updatedAt": true, "expiresAt": true, "contentType": true, "payload": true, "attributes": true}
	params := map[string]any{"select": projection, "limit": fmt.Sprintf("0x%x", options.Limit), "atBlock": fmt.Sprintf("0x%x", at)}
	if options.Cursor != "" {
		params["cursor"] = options.Cursor
	}
	var result struct {
		Data        []json.RawMessage `json:"data"`
		BlockNumber string            `json:"blockNumber"`
		Cursor      json.RawMessage   `json:"cursor"`
	}
	if err := c.call(ctx, "arkiv_query", []any{expression, params}, &result); err != nil {
		return nil, 0, "", err
	}
	block, err := quantity(result.BlockNumber)
	var cursor string
	if len(result.Cursor) > 0 && (bytes.Equal(result.Cursor, []byte("null")) || json.Unmarshal(result.Cursor, &cursor) != nil || cursor == "") {
		return nil, 0, "", ErrRPC
	}
	if err != nil || block != at || result.Data == nil || uint64(len(result.Data)) > options.Limit || len(cursor) > 2048 || (cursor != "" && cursor == options.Cursor) {
		return nil, 0, "", ErrRPC
	}
	return result.Data, block, cursor, nil
}

func (c *Client) call(ctx context.Context, method string, params []any, target any) error {
	id := c.nextID.Add(1)
	request, err := json.Marshal(struct {
		Version string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{"2.0", id, method, params})
	if err != nil {
		return fmt.Errorf("encode arkiv RPC request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(request))
	if err != nil {
		return ErrRPC
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Transport errors may embed access keys from configured RPC URLs.
		return ErrRPC
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("arkiv RPC HTTP status %d: %w", resp.StatusCode, ErrRPC)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return ErrRPC
	}
	var envelope struct {
		Version string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := decodeJSON(body, &envelope, true); err != nil || envelope.Version != "2.0" || envelope.ID != id {
		return ErrRPC
	}
	if len(envelope.Error) > 0 || len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		return ErrRPC
	}
	if err := decodeJSON(envelope.Result, target, false); err != nil {
		return errors.Join(ErrRPC, errors.New("invalid result shape"))
	}
	return nil
}
