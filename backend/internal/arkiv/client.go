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
	return NewClientWithHTTP(rpcURL, policy, nil)
}

func NewClientWithHTTP(rpcURL string, policy EndpointPolicy, transport *http.Client) (*Client, error) {
	if rpcURL == "" || policy == nil {
		return nil, ErrUnconfigured
	}
	if httpsURLSyntax(rpcURL) != nil {
		return nil, fmt.Errorf("invalid arkiv RPC URL: %w", ErrEndpoint)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if transport != nil {
		*client = *transport
	}
	if client.Timeout <= 0 || client.Timeout > 10*time.Second {
		client.Timeout = 10 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{rpcURL: rpcURL, policy: policy, now: time.Now, http: client}, nil
}

type QueryOptions struct {
	Capability     string
	MaxPriceMicros uint64
	Limit          uint64
	Offset         uint64
}

type OfferPage struct {
	Offers      []OfferSnapshot `json:"offers"`
	Rejected    int             `json:"rejected"`
	Offset      uint64          `json:"offset,string"`
	BlockNumber uint64          `json:"block_number,string"`
	Query       string          `json:"query"`
}

func (c *Client) Query(ctx context.Context, options QueryOptions) (OfferPage, error) {
	if options.Limit > 50 {
		return OfferPage{}, ErrInvalidOffer
	}
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
	expression += " ORDER BY $key ASC"
	raw, at, head, err := c.query(ctx, expression, options)
	if err != nil {
		return OfferPage{}, err
	}
	page := OfferPage{Offers: []OfferSnapshot{}, BlockNumber: at, Offset: options.Offset, Query: expression}
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
	entities, at, head, err := c.query(ctx, "$key = key("+strings.ToLower(entityKey)+")", QueryOptions{Limit: 1})
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
	if !strings.EqualFold(snapshot.EntityKey, entityKey) || snapshot.UpdatedAtBlock > at {
		return OfferSnapshot{}, ErrRPC
	}
	return snapshot, nil
}

func (c *Client) Revalidate(ctx context.Context, stored OfferSnapshot) (OfferSnapshot, error) {
	if stored.ArkivChainID != ChainID {
		return OfferSnapshot{}, ErrSnapshotNetwork
	}
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

func (c *Client) query(ctx context.Context, expression string, options QueryOptions) ([]json.RawMessage, uint64, uint64, error) {
	if options.Limit == 0 {
		options.Limit = 20
	}
	before, err := c.head(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	projection := map[string]bool{"key": true, "owner": true, "updatedAt": true, "expiresAt": true, "attributes": true}
	params := map[string]any{"select": projection, "limit": fmt.Sprintf("0x%x", options.Limit), "offset": fmt.Sprintf("0x%x", options.Offset)}
	var result struct {
		Data        []json.RawMessage `json:"data"`
		BlockNumber string            `json:"blockNumber"`
	}
	if err := c.call(ctx, "arkiv_query", []any{expression, params}, &result); err != nil {
		return nil, 0, 0, err
	}
	block, err := quantity(result.BlockNumber)
	if err != nil || block < before || result.Data == nil || uint64(len(result.Data)) > options.Limit {
		return nil, 0, 0, ErrRPC
	}
	// Rabdi has no historical reads. Check expiry against a fresh head after
	// the latest query, allowing the chain to advance while the request runs.
	after, err := c.head(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	if block > after {
		return nil, 0, 0, ErrRPC
	}
	return result.Data, block, after, nil
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
