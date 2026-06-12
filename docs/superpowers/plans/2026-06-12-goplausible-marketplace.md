# GoPlausible Algorand x402 Marketplace Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate GoPlausible's Algorand-native x402 facilitator as a second marketplace source, upgrade the x402 payment flow to the v2 spec so agent Algorand wallets can actually pay GoPlausible-registered endpoints, and surface chain-family badges in the UI so users know which endpoints their ALGO wallet can pay.

**Architecture:** GoPlausible runs a multi-chain x402 facilitator at `https://facilitator.goplausible.xyz` with a Bazaar-style discovery catalog (`GET /discovery/resources`) that currently has 0 items but is live and growing. We add a new backend handler that proxies this catalog, normalized to the same `BazaarEndpoint` shape already used for Coinbase Bazaar, with a new `chainFamily` field (`"avm"` for Algorand, `"evm"` for Base). In the frontend we show both catalogs side-by-side with clear chain badges. In the payment engine we upgrade `ExecuteTool402` from the current custom `X-Payment-Txid` header to the x402 v2 standard `X-PAYMENT` header (base64-encoded JSON payload with a signed Algorand msgpack transaction inside) so GoPlausible endpoints can actually verify and settle payments.

**Tech Stack:** Go 1.22, chi/v5, go-algorand-sdk/v2, Next.js 16 (App Router), React 19, TypeScript. No new Go dependencies required — `crypto.SignTransaction` in the Algorand SDK already returns msgpack-encoded signed bytes. The `encoding/base64` and `encoding/json` standard library packages handle the rest.

---

## Context You Must Know Before Starting

### GoPlausible Facilitator API (live, no auth required)

Base URL: `https://facilitator.goplausible.xyz`

| Endpoint | Method | Purpose |
|---|---|---|
| `/discovery/resources` | GET | List cataloged x402 resources |
| `/discovery/merchants` | GET | List merchants (name, logo, addresses) |
| `/discovery/paymentmethods` | GET | Per-network token/fee info |
| `/discovery/all` | GET | Aggregated summary |
| `/supported` | GET | Supported schemes and networks |
| `/verify` | POST | Verify a payment payload |
| `/settle` | POST | Settle (broadcast) a verified payment |

Query params for `/discovery/resources`: `limit` (int, default 50), `offset` (int, default 0), `type` (string, optional filter).

### Real API response for `GET /discovery/resources`

```json
{
  "x402Version": 2,
  "items": [
    {
      "id": "uuid-string",
      "resourceUrl": "https://api.example.com/weather",
      "method": "GET",
      "description": "Weather data by city",
      "mimeType": "application/json",
      "merchantId": "merchant-uuid",
      "accepts": [
        {
          "scheme": "exact",
          "network": "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
          "amount": "1000",
          "payTo": "ALGO_ADDRESS_HERE",
          "extra": { "feePayer": "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA" }
        }
      ],
      "discoveryInfo": {
        "input": { "method": "GET", "queryParams": { "city": "San Francisco" } },
        "output": { "example": { "temperature": 18.5 } }
      },
      "verifyCount": 0,
      "settleCount": 0,
      "firstSeen": "2026-06-12T00:00:00Z",
      "lastSeen": "2026-06-12T00:00:00Z"
    }
  ],
  "pagination": { "limit": 50, "offset": 0, "total": 0 }
}
```

### Real API response for `GET /discovery/merchants`

```json
{
  "x402Version": 2,
  "items": [
    {
      "id": "merchant-uuid",
      "name": "Weather Co",
      "description": "Real-time weather APIs",
      "website": "https://weatherco.example.com",
      "logo": "https://weatherco.example.com/logo.png",
      "addresses": { "evm": "0x...", "svm": "...", "avm": "ALGO_ADDRESS..." },
      "categories": ["data", "weather"],
      "resourceCount": 2,
      "totalVerifications": 0,
      "networks": ["algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI="],
      "firstSeen": "2026-06-12T00:00:00Z",
      "lastSeen": "2026-06-12T00:00:00Z"
    }
  ],
  "pagination": { "limit": 50, "offset": 0, "total": 0 }
}
```

### Algorand network identifiers (CAIP-2 format used by GoPlausible)

- Testnet: `algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=`
- Mainnet: `algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=`
- v1 compat: `algorand-testnet`, `algorand-mainnet`

GoPlausible feePayer (pays transaction fees for agent, gasless): `ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA`

### x402 v2 Payment Header Format for Algorand

When an endpoint returns 402, the agent must send `X-PAYMENT: <base64>` on retry. The base64-decoded value is a JSON object:

```json
{
  "x402Version": 2,
  "scheme": "exact",
  "network": "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
  "payload": {
    "paymentGroup": "<base64(msgpack-signed-txn-bytes)>",
    "paymentIndex": 0
  }
}
```

`paymentGroup` is `base64.StdEncoding.EncodeToString(signedTxnBytes)` where `signedTxnBytes` are the bytes returned by `crypto.SignTransaction()` — already msgpack-encoded by the Go SDK.

### Current state of tool402.go (the thing being upgraded)

`ExecuteTool402` currently:
1. GETs endpoint → 402
2. Calls `parsePaymentHeader()` → extracts `price`, `recipient` from flat map
3. Calls `signer.SignAndSendPayment()` → broadcasts on Algorand, returns txID
4. Retries with `X-Payment-Txid: <txID>` header ← **non-standard, GoPlausible won't accept this**

The upgrade changes step 3-4 to:
3. Calls `signer.BuildSignedPaymentBytes()` → signs but does NOT broadcast, returns msgpack bytes
4. Builds x402 v2 PaymentPayload JSON → base64 encodes it
5. Retries with `X-PAYMENT: <base64>` header ← **x402 v2 standard, GoPlausible accepts this**

### Current WalletSigner interface (in tool402.go)

```go
type WalletSigner interface {
    SignAndSendPayment(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) (string, error)
}
```

We add a second method. `wallet.Service` must implement both. In tests, the mock struct must also implement both.

### Key existing files and their line ranges

| File | What's relevant |
|---|---|
| `backend/internal/api/handlers/marketplace.go` | `BazaarEndpoint` struct (line 51-63), `normalizeBazaarItem` (line 146), `fetchBazaar` (line 105) |
| `backend/internal/api/handlers/marketplace_test.go` | Pattern for fake server tests |
| `backend/internal/wallet/algorand.go` | `SignAndSendPayment` (line 91-126) |
| `backend/internal/engine/nodes/tool402.go` | `WalletSigner` interface (line 14-17), `ExecuteTool402` (line 39-99), `parsePaymentHeader` (line 101-127) |
| `backend/internal/api/router.go` | `r.Get("/marketplace/bazaar", ...)` (line 32-33) |
| `frontend/src/lib/types.ts` | `MarketplaceEndpoint` (line 100-118) |
| `frontend/src/lib/api.ts` | `marketplace.bazaarList` (line 244-270) |
| `frontend/src/components/marketplace/MarketplacePage.tsx` | Bazaar section (line 146-170) |

---

## File Map

| File | Create / Modify | Responsibility |
|---|---|---|
| `backend/internal/api/handlers/goplausible.go` | **Create** | GoPlausible catalog list handler; normalizes CatalogedResource + enriches with merchant data |
| `backend/internal/api/handlers/goplausible_test.go` | **Create** | Tests for GoplausibleList using a fake facilitator HTTP server |
| `backend/internal/api/handlers/marketplace.go` | **Modify** | Add `ChainFamily string` field to `BazaarEndpoint`; set it to `"evm"` in `normalizeBazaarItem` |
| `backend/internal/api/router.go` | **Modify** | Register `GET /marketplace/goplausible` and `GET /marketplace/goplausible/merchants` |
| `backend/internal/wallet/algorand.go` | **Modify** | Add `BuildSignedPaymentBytes()` — signs without broadcasting |
| `backend/internal/engine/nodes/tool402.go` | **Modify** | Expand `WalletSigner` interface; upgrade `ExecuteTool402` to x402 v2 `X-PAYMENT` header; update `parsePaymentHeader` to handle x402 v2 structured body |
| `frontend/src/lib/types.ts` | **Modify** | Add `chainFamily?: "avm" \| "evm" \| "svm"` to `MarketplaceEndpoint`; add `"goplausible"` to `source` union |
| `frontend/src/lib/api.ts` | **Modify** | Add `marketplace.goplausibleList()` |
| `frontend/src/components/marketplace/MarketplacePage.tsx` | **Modify** | Fetch GoPlausible catalog alongside Bazaar; add "Algorand Compatible" section; chain badge on each card |

---

## Task 1: Add `ChainFamily` to `BazaarEndpoint` and tag Coinbase items as `"evm"`

**Files:**
- Modify: `backend/internal/api/handlers/marketplace.go`
- Modify: `backend/internal/api/handlers/marketplace_test.go`

This is the smallest change — extend the shared struct before the new handler needs it.

- [ ] **Step 1: Write the failing test**

Open `backend/internal/api/handlers/marketplace_test.go`. Add this test at the bottom:

```go
func TestBazaarListChainFamily(t *testing.T) {
    fake := fakeBazaarServer(t)
    defer fake.Close()
    t.Setenv("BAZAAR_BASE_URL", fake.URL)

    d := &handlers.Deps{}
    req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar", nil)
    w := httptest.NewRecorder()
    d.BazaarList(w, req)

    var resp map[string]any
    json.NewDecoder(w.Body).Decode(&resp)
    eps, _ := resp["endpoints"].([]any)
    if len(eps) == 0 {
        t.Fatal("want at least 1 endpoint")
    }
    ep, _ := eps[0].(map[string]any)
    if ep["chainFamily"] != "evm" {
        t.Errorf("want chainFamily=evm for Coinbase Bazaar items, got %v", ep["chainFamily"])
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend && go test ./internal/api/handlers/... -run TestBazaarListChainFamily -v
```

Expected: `FAIL` — `want chainFamily=evm for Coinbase Bazaar items, got <nil>`

- [ ] **Step 3: Add `ChainFamily` to `BazaarEndpoint` in `marketplace.go`**

In `backend/internal/api/handlers/marketplace.go`, find the `BazaarEndpoint` struct (around line 51) and add the field:

```go
type BazaarEndpoint struct {
    ID               string            `json:"id"`
    Name             string            `json:"name"`
    Description      string            `json:"description"`
    Provider         string            `json:"provider"`
    Price            string            `json:"price"`
    Unit             string            `json:"unit"`
    Category         string            `json:"category"`
    Tags             []string          `json:"tags"`
    Endpoint         string            `json:"endpoint"`
    DiscoveredParams []models.ParamDef `json:"discoveredParams,omitempty"`
    Source           string            `json:"source"`
    ChainFamily      string            `json:"chainFamily"` // "evm", "avm", "svm"
}
```

- [ ] **Step 4: Set `ChainFamily: "evm"` in `normalizeBazaarItem`**

In `normalizeBazaarItem` (around line 146), add the field to the returned struct:

```go
return BazaarEndpoint{
    ID:               id,
    Name:             name,
    Description:      item.Description,
    Provider:         provider,
    Price:            formatBazaarPrice(item.Accepts),
    Unit:             "call",
    Category:         categoryFromTags(item.Tags, item.Type),
    Tags:             item.Tags,
    Endpoint:         item.Resource,
    DiscoveredParams: extractBazaarParams(item.Extensions),
    Source:           "bazaar",
    ChainFamily:      "evm",
}
```

- [ ] **Step 5: Run all marketplace tests to confirm they pass**

```bash
cd backend && go test ./internal/api/handlers/... -run TestBazaar -v
```

Expected: 4 tests pass — `TestBazaarList`, `TestBazaarSearch`, `TestBazaarSearchMissingQ`, `TestBazaarListChainFamily`

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/handlers/marketplace.go backend/internal/api/handlers/marketplace_test.go
git commit -m "feat: add chainFamily field to BazaarEndpoint, tag Coinbase items as evm"
```

---

## Task 2: Create GoPlausible discovery handler

**Files:**
- Create: `backend/internal/api/handlers/goplausible.go`
- Create: `backend/internal/api/handlers/goplausible_test.go`

This is the main backend work. The handler fetches from GoPlausible's facilitator, joins resource items with merchant data (to get names and logos), and returns the same `BazaarEndpoint` shape with `source: "goplausible"` and `chainFamily: "avm"` (since all GoPlausible Algorand items use AVM).

- [ ] **Step 1: Write the failing tests first**

Create `backend/internal/api/handlers/goplausible_test.go`:

```go
package handlers_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/agentmesh/backend/internal/api/handlers"
)

// fakeMerchant is the GoPlausible merchant shape.
var fakeMerchant = map[string]any{
    "id":          "merchant-1",
    "name":        "Weather Co",
    "description": "Real-time weather APIs",
    "website":     "https://weatherco.example.com",
    "logo":        "",
    "addresses":   map[string]any{"avm": "ALGO_ADDR_HERE"},
    "categories":  []string{"data"},
    "resourceCount": 1,
    "networks":    []string{"algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI="},
}

// fakeResource is the GoPlausible CatalogedResource shape.
var fakeResource = map[string]any{
    "id":          "resource-1",
    "resourceUrl": "https://api.weatherco.example.com/weather",
    "method":      "GET",
    "description": "Get weather by city",
    "mimeType":    "application/json",
    "merchantId":  "merchant-1",
    "accepts": []map[string]any{
        {
            "scheme":  "exact",
            "network": "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
            "amount":  "1000",
            "payTo":   "ALGO_ADDR_HERE",
        },
    },
    "discoveryInfo": map[string]any{
        "input": map[string]any{
            "method": "GET",
            "queryParams": map[string]any{
                "city": "San Francisco",
            },
        },
        "output": map[string]any{
            "example": map[string]any{"temperature": 18.5},
        },
    },
    "verifyCount": 0,
    "settleCount": 0,
}

// fakeGoPlausibleServer returns an httptest.Server that responds
// to both /discovery/resources and /discovery/merchants with fake data.
func fakeGoPlausibleServer(t *testing.T) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if strings.Contains(r.URL.Path, "merchants") {
            json.NewEncoder(w).Encode(map[string]any{
                "x402Version": 2,
                "items":       []any{fakeMerchant},
                "pagination":  map[string]any{"limit": 50, "offset": 0, "total": 1},
            })
            return
        }
        // /discovery/resources (default)
        json.NewEncoder(w).Encode(map[string]any{
            "x402Version": 2,
            "items":       []any{fakeResource},
            "pagination":  map[string]any{"limit": 50, "offset": 0, "total": 1},
        })
    }))
}

func TestGoplausibleList(t *testing.T) {
    fake := fakeGoPlausibleServer(t)
    defer fake.Close()
    t.Setenv("GOPLAUSIBLE_BASE_URL", fake.URL)

    d := &handlers.Deps{}
    req := httptest.NewRequest(http.MethodGet, "/marketplace/goplausible", nil)
    w := httptest.NewRecorder()
    d.GoplausibleList(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
    }
    var resp map[string]any
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    eps, _ := resp["endpoints"].([]any)
    if len(eps) != 1 {
        t.Fatalf("want 1 endpoint got %d", len(eps))
    }
    ep, _ := eps[0].(map[string]any)

    if ep["source"] != "goplausible" {
        t.Errorf("want source=goplausible got %v", ep["source"])
    }
    if ep["chainFamily"] != "avm" {
        t.Errorf("want chainFamily=avm got %v", ep["chainFamily"])
    }
    if ep["endpoint"] != "https://api.weatherco.example.com/weather" {
        t.Errorf("want endpoint URL got %v", ep["endpoint"])
    }
    if ep["name"] != "Weather Co" {
        t.Errorf("want name=Weather Co (from merchant) got %v", ep["name"])
    }
    if ep["price"] != "0.0010" {
        t.Errorf("want price=0.0010 got %v", ep["price"])
    }
}

func TestGoplausibleListEmpty(t *testing.T) {
    // When catalog is empty, returns empty array (not an error).
    fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "x402Version": 2,
            "items":       []any{},
            "pagination":  map[string]any{"limit": 50, "offset": 0, "total": 0},
        })
    }))
    defer fake.Close()
    t.Setenv("GOPLAUSIBLE_BASE_URL", fake.URL)

    d := &handlers.Deps{}
    req := httptest.NewRequest(http.MethodGet, "/marketplace/goplausible", nil)
    w := httptest.NewRecorder()
    d.GoplausibleList(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("want 200 got %d", w.Code)
    }
    var resp map[string]any
    json.NewDecoder(w.Body).Decode(&resp)
    eps, _ := resp["endpoints"].([]any)
    if len(eps) != 0 {
        t.Errorf("want 0 endpoints got %d", len(eps))
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/api/handlers/... -run TestGoplausible -v
```

Expected: `FAIL` — `d.GoplausibleList undefined`

- [ ] **Step 3: Create `goplausible.go`**

Create `backend/internal/api/handlers/goplausible.go`:

```go
package handlers

import (
    "context"
    "crypto/md5"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/agentmesh/backend/internal/models"
    "github.com/agentmesh/backend/internal/respond"
)

var goplausibleHTTPClient = &http.Client{Timeout: 15 * time.Second}

// goplausibleBase returns the GoPlausible facilitator base URL.
// Override with GOPLAUSIBLE_BASE_URL env var in tests.
func goplausibleBase() string {
    if u := os.Getenv("GOPLAUSIBLE_BASE_URL"); u != "" {
        return u
    }
    return "https://facilitator.goplausible.xyz"
}

// gpResource is the CatalogedResource shape from the GoPlausible facilitator.
type gpResource struct {
    ID            string           `json:"id"`
    ResourceURL   string           `json:"resourceUrl"`
    Method        string           `json:"method"`
    Description   string           `json:"description"`
    MimeType      string           `json:"mimeType"`
    MerchantID    string           `json:"merchantId"`
    Accepts       []gpAccept       `json:"accepts"`
    DiscoveryInfo map[string]any   `json:"discoveryInfo"`
    VerifyCount   int              `json:"verifyCount"`
    SettleCount   int              `json:"settleCount"`
}

type gpAccept struct {
    Scheme  string `json:"scheme"`
    Network string `json:"network"`
    Amount  string `json:"amount"`
    PayTo   string `json:"payTo"`
}

// gpMerchant is the CatalogedMerchant shape from the GoPlausible facilitator.
type gpMerchant struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Website     string   `json:"website"`
    Logo        string   `json:"logo"`
    Categories  []string `json:"categories"`
}

type gpResourcesResp struct {
    Items []gpResource `json:"items"`
}

type gpMerchantsResp struct {
    Items []gpMerchant `json:"items"`
}

// GoplausibleList proxies GET /marketplace/goplausible → GoPlausible /discovery/resources.
// It enriches each resource with merchant name/description from /discovery/merchants.
func (d *Deps) GoplausibleList(w http.ResponseWriter, r *http.Request) {
    limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
    if err != nil || limit <= 0 || limit > 100 {
        limit = 50
    }
    offset, err2 := strconv.Atoi(r.URL.Query().Get("offset"))
    if err2 != nil || offset < 0 {
        offset = 0
    }

    ctx := r.Context()
    resources, err := fetchGPResources(ctx, limit, offset)
    if err != nil {
        respond.Error(w, http.StatusBadGateway, "goplausible unavailable")
        return
    }

    // Fetch merchants once to enrich resource items with provider name.
    merchants, _ := fetchGPMerchants(ctx)
    merchantMap := make(map[string]gpMerchant, len(merchants))
    for _, m := range merchants {
        merchantMap[m.ID] = m
    }

    endpoints := make([]BazaarEndpoint, 0, len(resources))
    for _, res := range resources {
        ep := normalizeGPResource(res, merchantMap)
        if ep.Endpoint != "" {
            endpoints = append(endpoints, ep)
        }
    }
    respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

func fetchGPResources(ctx context.Context, limit, offset int) ([]gpResource, error) {
    url := fmt.Sprintf("%s/discovery/resources?limit=%d&offset=%d", goplausibleBase(), limit, offset)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Accept", "application/json")

    resp, err := goplausibleHTTPClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
        return nil, fmt.Errorf("goplausible %d: %s", resp.StatusCode, string(b))
    }

    var raw gpResourcesResp
    if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
        return nil, fmt.Errorf("goplausible decode: %w", err)
    }
    return raw.Items, nil
}

func fetchGPMerchants(ctx context.Context) ([]gpMerchant, error) {
    url := goplausibleBase() + "/discovery/merchants?limit=200"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Accept", "application/json")

    resp, err := goplausibleHTTPClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return nil, nil // merchant enrichment is best-effort
    }

    var raw gpMerchantsResp
    json.NewDecoder(resp.Body).Decode(&raw)
    return raw.Items, nil
}

// normalizeGPResource converts a GoPlausible CatalogedResource into the shared
// BazaarEndpoint shape. Uses merchant data when available for name/description.
func normalizeGPResource(res gpResource, merchants map[string]gpMerchant) BazaarEndpoint {
    id := fmt.Sprintf("goplausible-%x", md5.Sum([]byte(res.ResourceURL)))

    name := res.Description
    provider := ""
    if m, ok := merchants[res.MerchantID]; ok {
        provider = m.Name
        if m.Name != "" {
            name = m.Name
        }
    }
    if name == "" {
        name = pathToName(urlPath(res.ResourceURL))
    }
    if name == "" {
        name = provider
    }

    chainFamily := gpChainFamily(res.Accepts)

    return BazaarEndpoint{
        ID:               id,
        Name:             name,
        Description:      res.Description,
        Provider:         provider,
        Price:            gpFormatPrice(res.Accepts),
        Unit:             "call",
        Category:         gpCategory(res),
        Tags:             nil,
        Endpoint:         res.ResourceURL,
        DiscoveredParams: gpExtractParams(res.DiscoveryInfo),
        Source:           "goplausible",
        ChainFamily:      chainFamily,
    }
}

// gpChainFamily returns the chain family of the first accept entry.
// Returns "avm" for Algorand, "evm" for Base/Ethereum, "svm" for Solana.
func gpChainFamily(accepts []gpAccept) string {
    if len(accepts) == 0 {
        return "avm"
    }
    network := accepts[0].Network
    switch {
    case strings.HasPrefix(network, "algorand:") || strings.HasPrefix(network, "algorand-"):
        return "avm"
    case strings.HasPrefix(network, "eip155:"):
        return "evm"
    case strings.HasPrefix(network, "solana:"):
        return "svm"
    default:
        return "avm"
    }
}

// gpFormatPrice converts micro-USDC amount (6 decimals) to a dollar string.
func gpFormatPrice(accepts []gpAccept) string {
    if len(accepts) == 0 {
        return ""
    }
    amount, err := strconv.ParseFloat(accepts[0].Amount, 64)
    if err != nil || amount <= 0 {
        return ""
    }
    usd := amount / 1e6
    if usd < 0.001 {
        return fmt.Sprintf("%.6f", usd)
    }
    if usd < 0.01 {
        return fmt.Sprintf("%.4f", usd)
    }
    return fmt.Sprintf("%.4f", usd)
}

// gpCategory derives a category from the resource description and discoveryInfo.
func gpCategory(res gpResource) string {
    combined := strings.ToLower(res.Description + " " + res.MimeType)
    switch {
    case strings.Contains(combined, "search") || strings.Contains(combined, "query"):
        return "search"
    case strings.Contains(combined, "finance") || strings.Contains(combined, "crypto") ||
        strings.Contains(combined, "price") || strings.Contains(combined, "market"):
        return "finance"
    case strings.Contains(combined, "image") || strings.Contains(combined, "video") ||
        strings.Contains(combined, "audio"):
        return "media"
    case strings.Contains(combined, "ai") || strings.Contains(combined, "nlp") ||
        strings.Contains(combined, "sentiment"):
        return "ai"
    case strings.Contains(combined, "weather") || strings.Contains(combined, "data") ||
        strings.Contains(combined, "news"):
        return "data"
    default:
        return "util"
    }
}

// gpExtractParams reads query param definitions from discoveryInfo.input.queryParams.
func gpExtractParams(info map[string]any) []models.ParamDef {
    if info == nil {
        return nil
    }
    input, _ := info["input"].(map[string]any)
    if input == nil {
        return nil
    }
    qp, _ := input["queryParams"].(map[string]any)
    if len(qp) == 0 {
        return nil
    }
    params := make([]models.ParamDef, 0, len(qp))
    for name, v := range qp {
        switch v.(type) {
        case string:
            // discoveryInfo.input.queryParams contains example values, not schema.
            // Infer type from value.
            params = append(params, models.ParamDef{Name: name, Type: "string", Required: false})
        default:
            params = append(params, models.ParamDef{Name: name, Type: "string", Required: false})
        }
    }
    return params
}

// urlPath returns the path component of a URL string, or empty string on failure.
func urlPath(rawURL string) string {
    // Quick path extraction without full import of net/url here — reuse net/url already imported.
    // Find path after host.
    s := rawURL
    if i := strings.Index(s, "://"); i >= 0 {
        s = s[i+3:]
    }
    if i := strings.Index(s, "/"); i >= 0 {
        return s[i:]
    }
    return ""
}
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/api/handlers/... -run TestGoplausible -v
```

Expected: Both `TestGoplausibleList` and `TestGoplausibleListEmpty` pass.

- [ ] **Step 5: Run the full handler test suite to check nothing broke**

```bash
cd backend && go test ./internal/api/handlers/... -v
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/handlers/goplausible.go backend/internal/api/handlers/goplausible_test.go
git commit -m "feat: add GoPlausible discovery handler with merchant enrichment"
```

---

## Task 3: Wire GoPlausible routes into the router

**Files:**
- Modify: `backend/internal/api/router.go`

Short task — just two new `r.Get` calls.

- [ ] **Step 1: Add the routes**

Open `backend/internal/api/router.go`. Find the marketplace public routes block (around line 31-33):

```go
// Marketplace — public so the page loads without auth
r.Get("/marketplace/bazaar", d.BazaarList)
r.Get("/marketplace/bazaar/search", d.BazaarSearch)
```

Replace it with:

```go
// Marketplace — public so the page loads without auth
r.Get("/marketplace/bazaar", d.BazaarList)
r.Get("/marketplace/bazaar/search", d.BazaarSearch)
r.Get("/marketplace/goplausible", d.GoplausibleList)
```

- [ ] **Step 2: Build to confirm no compile errors**

```bash
cd backend && go build ./...
```

Expected: builds with no output.

- [ ] **Step 3: Smoke test the endpoint against the live GoPlausible API**

```bash
curl -s "http://localhost:8080/marketplace/goplausible?limit=10" | python3 -m json.tool
```

Expected: `{"endpoints": []}` — catalog is empty but endpoint responds 200. When GoPlausible merchants register, items will appear here.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/router.go
git commit -m "feat: register GET /marketplace/goplausible route"
```

---

## Task 4: Add `BuildSignedPaymentBytes` to the wallet service

**Files:**
- Modify: `backend/internal/wallet/algorand.go`

This adds a sign-without-broadcast method. The existing `SignAndSendPayment` is NOT removed — it's still used by the old test-x402 flow. The new method is for x402 v2 endpoints.

- [ ] **Step 1: Write the failing test**

Open `backend/internal/wallet/algorand_test.go`. Add:

```go
func TestBuildSignedPaymentBytes_ReturnsNonEmptyBytes(t *testing.T) {
    // This test is intentionally minimal — we can't call a live algod in unit tests.
    // We verify that BuildSignedPaymentBytes exists on *Service and has the right signature.
    // Integration testing against testnet is done manually.
    var _ func(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) ([]byte, error) =
        (&Service{}).BuildSignedPaymentBytes
}
```

- [ ] **Step 2: Run to confirm it fails**

```bash
cd backend && go test ./internal/wallet/... -run TestBuildSignedPaymentBytes -v
```

Expected: `FAIL` — `(*Service).BuildSignedPaymentBytes undefined`

- [ ] **Step 3: Implement `BuildSignedPaymentBytes` in `algorand.go`**

Open `backend/internal/wallet/algorand.go`. After `SignAndSendPayment` (which ends around line 126), add:

```go
// BuildSignedPaymentBytes creates a signed Algorand payment transaction and returns
// the raw msgpack-encoded signed transaction bytes. It does NOT broadcast —
// the GoPlausible facilitator broadcasts the transaction during /settle.
// The returned bytes can be base64-encoded and embedded in an x402 v2 X-PAYMENT header.
func (s *Service) BuildSignedPaymentBytes(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) ([]byte, error) {
    mn, err := s.DecryptMnemonic(encMnemonic)
    if err != nil {
        return nil, err
    }
    privKey, err := mnemonic.ToPrivateKey(mn)
    if err != nil {
        return nil, err
    }
    acc, err := crypto.AccountFromPrivateKey(privKey)
    if err != nil {
        return nil, err
    }

    client, err := algod.MakeClient(s.algodURL, s.algodToken)
    if err != nil {
        return nil, err
    }
    params, err := client.SuggestedParams().Do(ctx)
    if err != nil {
        return nil, err
    }
    txn, err := transaction.MakePaymentTxn(acc.Address.String(), toAddress, microAlgo, nil, "", params)
    if err != nil {
        return nil, err
    }
    _, signed, err := crypto.SignTransaction(privKey, txn)
    if err != nil {
        return nil, err
    }
    // signed is already msgpack-encoded by the SDK — ready for base64 encoding.
    return signed, nil
}
```

- [ ] **Step 4: Run the test**

```bash
cd backend && go test ./internal/wallet/... -run TestBuildSignedPaymentBytes -v
```

Expected: `PASS` — compilation confirms the method signature is correct.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/wallet/algorand.go backend/internal/wallet/algorand_test.go
git commit -m "feat: add BuildSignedPaymentBytes to wallet service for x402 v2 payment"
```

---

## Task 5: Upgrade `ExecuteTool402` to x402 v2 `X-PAYMENT` header

**Files:**
- Modify: `backend/internal/engine/nodes/tool402.go`

This is the core payment flow upgrade. The changes are:
1. Expand `WalletSigner` interface with `BuildSignedPaymentBytes`
2. Update `parsePaymentHeader` to handle both old flat format and x402 v2 structured format
3. In `ExecuteTool402`, detect Algorand network and build proper `X-PAYMENT` header

**Critical:** After this change, any Go file that has a struct implementing `WalletSigner` must add the new method. Search for `WalletSigner` across the codebase before starting.

- [ ] **Step 0: Find all WalletSigner implementors**

```bash
cd backend && grep -rn "WalletSigner" --include="*.go"
```

Expected output shows: `tool402.go` (definition), `provider.go` (usage), and possibly test files. If any test file defines a mock struct for `WalletSigner`, it must be updated in this task.

- [ ] **Step 1: Write the failing test**

Open `backend/internal/engine/nodes/action_useours_test.go` (or whichever test file mocks WalletSigner). Add a test specifically for the v2 header path.

First, create `backend/internal/engine/nodes/tool402_v2_test.go`:

```go
package nodes_test

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/agentmesh/backend/internal/engine/nodes"
    "github.com/agentmesh/backend/internal/models"
)

// mockV2Signer implements WalletSigner for testing. Returns deterministic fake bytes.
type mockV2Signer struct {
    builtBytes []byte // set to non-nil to simulate BuildSignedPaymentBytes success
}

func (m *mockV2Signer) SignAndSendPayment(_ context.Context, _, _ string, _ uint64) (string, error) {
    return "FAKE_TXID", nil
}

func (m *mockV2Signer) BuildSignedPaymentBytes(_ context.Context, _, _ string, _ uint64) ([]byte, error) {
    if m.builtBytes != nil {
        return m.builtBytes, nil
    }
    return []byte("fake-msgpack-signed-txn"), nil
}

// algorandPaymentHeader is what the fake Algorand x402 resource server sends as 402 body.
var algorandPaymentHeader = map[string]any{
    "x402Version": 2,
    "accepts": []map[string]any{
        {
            "scheme":  "exact",
            "network": "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
            "amount":  "1000",
            "payTo":   "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA",
        },
    },
    "resource": "https://example.com/api/weather",
}

func TestExecuteTool402_AlgorandV2Header(t *testing.T) {
    var receivedPaymentHeader string

    // Fake resource server: first request → 402, second request → 200 with X-PAYMENT header captured.
    calls := 0
    fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if calls == 1 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusPaymentRequired)
            json.NewEncoder(w).Encode(algorandPaymentHeader)
            return
        }
        // Second call — capture what header the client sent.
        receivedPaymentHeader = r.Header.Get("X-PAYMENT")
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
    }))
    defer fake.Close()

    node := models.WorkflowNode{Endpoint: fake.URL + "/api/weather"}
    wallet := models.AgentWallet{EncryptedMnemonic: "enc-mnemonic"}
    signer := &mockV2Signer{}

    result, err := nodes.ExecuteTool402(context.Background(), node, nil, wallet, signer)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    _ = result

    if receivedPaymentHeader == "" {
        t.Fatal("want X-PAYMENT header set on retry, got empty")
    }

    // Decode the X-PAYMENT header and verify it's a valid x402 v2 payload for Algorand.
    raw, err := base64.StdEncoding.DecodeString(receivedPaymentHeader)
    if err != nil {
        t.Fatalf("X-PAYMENT not valid base64: %v", err)
    }
    var payload map[string]any
    if err := json.Unmarshal(raw, &payload); err != nil {
        t.Fatalf("X-PAYMENT not valid JSON: %v", err)
    }
    if payload["x402Version"].(float64) != 2 {
        t.Errorf("want x402Version=2 got %v", payload["x402Version"])
    }
    if payload["scheme"] != "exact" {
        t.Errorf("want scheme=exact got %v", payload["scheme"])
    }
    if !strings.HasPrefix(payload["network"].(string), "algorand:") {
        t.Errorf("want algorand: network got %v", payload["network"])
    }
    innerPayload, _ := payload["payload"].(map[string]any)
    if innerPayload == nil {
        t.Fatal("want payload object in X-PAYMENT JSON")
    }
    if _, ok := innerPayload["paymentGroup"]; !ok {
        t.Error("want paymentGroup in payload")
    }
    if innerPayload["paymentIndex"].(float64) != 0 {
        t.Errorf("want paymentIndex=0 got %v", innerPayload["paymentIndex"])
    }
}
```

- [ ] **Step 2: Run to confirm test fails**

```bash
cd backend && go test ./internal/engine/nodes/... -run TestExecuteTool402_AlgorandV2Header -v
```

Expected: `FAIL` — either compile error (mockV2Signer missing method) or logic failure.

- [ ] **Step 3: Expand `WalletSigner` interface in `tool402.go`**

Open `backend/internal/engine/nodes/tool402.go`. Replace the `WalletSigner` interface (lines 14-17):

```go
// WalletSigner signs and submits Algorand payment transactions.
// Satisfied by *wallet.Service.
type WalletSigner interface {
    // SignAndSendPayment broadcasts an Algorand payment and returns the transaction ID.
    // Used for legacy custom x402 servers that expect X-Payment-Txid.
    SignAndSendPayment(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) (string, error)
    // BuildSignedPaymentBytes signs an Algorand payment and returns the raw msgpack bytes
    // WITHOUT broadcasting. Used for x402 v2 endpoints (X-PAYMENT header).
    BuildSignedPaymentBytes(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) ([]byte, error)
}
```

- [ ] **Step 4: Add the x402 v2 PaymentPayload types near the top of `tool402.go`**

After the imports block and before `QuoteX402`, add:

```go
// x402V2Payload is the JSON structure sent as the X-PAYMENT header value (base64-encoded).
type x402V2Payload struct {
    X402Version int                `json:"x402Version"`
    Scheme      string             `json:"scheme"`
    Network     string             `json:"network"`
    Payload     x402AlgorandInner `json:"payload"`
}

type x402AlgorandInner struct {
    PaymentGroup string `json:"paymentGroup"` // base64(msgpack-signed-txn-bytes)
    PaymentIndex int    `json:"paymentIndex"` // always 0 (payment is first/only txn)
}
```

Also add `"encoding/base64"` to the imports block if not already present.

- [ ] **Step 5: Update `parsePaymentHeader` to handle x402 v2 structured body**

Replace the entire `parsePaymentHeader` function with:

```go
// parsePaymentHeader extracts payment requirements from a 402 response.
// Handles two formats:
//   - x402 v2: JSON body with {"x402Version":2,"accepts":[{scheme,network,amount,payTo}]}
//   - Legacy flat: JSON body/header with {"price","recipient","network"} (our test-x402 format)
func parsePaymentHeader(resp *http.Response) map[string]any {
    header := resp.Header.Get("X-Payment-Required")
    if header == "" {
        header = resp.Header.Get("WWW-Authenticate")
    }

    body, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))

    // Try x402 v2 structured format first (body takes precedence).
    var v2 struct {
        X402Version int              `json:"x402Version"`
        Accepts     []map[string]any `json:"accepts"`
        Resource    string           `json:"resource"`
    }
    if err := json.Unmarshal(body, &v2); err == nil && v2.X402Version == 2 && len(v2.Accepts) > 0 {
        accept := v2.Accepts[0]
        amount, _ := accept["amount"].(string)
        payTo, _ := accept["payTo"].(string)
        network, _ := accept["network"].(string)
        scheme, _ := accept["scheme"].(string)
        if scheme == "" {
            scheme = "exact"
        }
        return map[string]any{
            "price":     amount,
            "recipient": payTo,
            "network":   network,
            "scheme":    scheme,
            "v2":        true, // signals to ExecuteTool402 to use X-PAYMENT header
        }
    }

    // Legacy flat format — header string.
    if header != "" {
        var result map[string]any
        if err := json.Unmarshal([]byte(header), &result); err == nil {
            return result
        }
    }

    // Body envelope fallback: {"error":"Payment required","payment":{...}}
    var envelope struct {
        Payment map[string]any `json:"payment"`
    }
    if err := json.Unmarshal(body, &envelope); err == nil && envelope.Payment != nil {
        return envelope.Payment
    }

    // Last resort: try body directly as payment object.
    var result map[string]any
    if err := json.Unmarshal(body, &result); err == nil {
        return result
    }
    return map[string]any{"raw": header}
}
```

- [ ] **Step 6: Update `ExecuteTool402` to use x402 v2 `X-PAYMENT` header for Algorand**

Replace the entire `ExecuteTool402` function with:

```go
func ExecuteTool402(ctx context.Context, node models.WorkflowNode, rc RunContexter, wallet models.AgentWallet, signer WalletSigner) (any, error) {
    if err := urlValidator(node.Endpoint); err != nil {
        return nil, err
    }
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
    resp, err := toolHTTPClient.Do(req)
    if err != nil {
        return nil, err
    }

    if resp.StatusCode != http.StatusPaymentRequired {
        defer resp.Body.Close()
        b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
        var result any
        if json.Unmarshal(b, &result) == nil {
            return result, nil
        }
        return string(b), nil
    }

    quote := parsePaymentHeader(resp) // reads and closes body internally
    resp.Body.Close()

    if wallet.EncryptedMnemonic == "" || signer == nil {
        return map[string]any{"error": "payment required but no agent wallet configured", "quote": quote}, nil
    }

    priceStr, _ := quote["price"].(string)
    recipient, _ := quote["recipient"].(string)
    network, _ := quote["network"].(string)
    isV2, _ := quote["v2"].(bool)

    if recipient == "" {
        return nil, fmt.Errorf("x402: no recipient address in payment header")
    }
    priceFloat, err := strconv.ParseFloat(priceStr, 64)
    if err != nil || priceFloat <= 0 {
        return nil, fmt.Errorf("x402: invalid price %q", priceStr)
    }
    microAlgo := uint64(priceFloat)

    // Determine payment path: x402 v2 (X-PAYMENT header) vs legacy (X-Payment-Txid).
    isAlgorand := strings.HasPrefix(network, "algorand:")
    useV2Header := isV2 && isAlgorand && signer != nil

    var req2 *http.Request
    req2, _ = http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)

    if useV2Header {
        // x402 v2 path: sign without broadcast, embed in X-PAYMENT header.
        signedBytes, err := signer.BuildSignedPaymentBytes(ctx, wallet.EncryptedMnemonic, recipient, microAlgo)
        if err != nil {
            return nil, fmt.Errorf("x402 v2 signing failed: %w", err)
        }
        paymentGroup := base64.StdEncoding.EncodeToString(signedBytes)
        payload := x402V2Payload{
            X402Version: 2,
            Scheme:      "exact",
            Network:     network,
            Payload:     x402AlgorandInner{PaymentGroup: paymentGroup, PaymentIndex: 0},
        }
        payloadJSON, err := json.Marshal(payload)
        if err != nil {
            return nil, fmt.Errorf("x402 v2 marshal failed: %w", err)
        }
        req2.Header.Set("X-PAYMENT", base64.StdEncoding.EncodeToString(payloadJSON))
    } else {
        // Legacy path: broadcast Algorand txn, send txID as custom header.
        txID, err := signer.SignAndSendPayment(ctx, wallet.EncryptedMnemonic, recipient, microAlgo)
        if err != nil {
            return nil, fmt.Errorf("x402 payment failed: %w", err)
        }
        req2.Header.Set("X-Payment-Txid", txID)
        algoAmount := fmt.Sprintf("%.6f", float64(microAlgo)/1e6)
        explorerURL := "https://lora.algokit.io/testnet/transaction/" + txID
        resp2, err := toolHTTPClient.Do(req2)
        if err != nil {
            return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "error": "retry failed: " + err.Error()}, nil
        }
        defer resp2.Body.Close()
        b, _ := io.ReadAll(io.LimitReader(resp2.Body, httpResponseLimit))
        var retryResult any
        if json.Unmarshal(b, &retryResult) == nil {
            return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "response": retryResult}, nil
        }
        return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "response": string(b)}, nil
    }

    // x402 v2 retry.
    resp2, err := toolHTTPClient.Do(req2)
    if err != nil {
        return nil, fmt.Errorf("x402 v2 retry failed: %w", err)
    }
    defer resp2.Body.Close()
    b, _ := io.ReadAll(io.LimitReader(resp2.Body, httpResponseLimit))
    var retryResult any
    if json.Unmarshal(b, &retryResult) == nil {
        return map[string]any{"status": "payment_sent_v2", "network": network, "response": retryResult}, nil
    }
    return map[string]any{"status": "payment_sent_v2", "network": network, "response": string(b)}, nil
}
```

- [ ] **Step 7: Add `"encoding/base64"` to imports in `tool402.go`**

Open `backend/internal/engine/nodes/tool402.go` and ensure the imports block includes `"encoding/base64"` and `"strings"`:

```go
import (
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strconv"
    "strings"

    "github.com/agentmesh/backend/internal/models"
)
```

- [ ] **Step 8: Update any mock WalletSigner structs in test files**

Run this to find all mock structs:

```bash
cd backend && grep -rn "SignAndSendPayment" --include="*_test.go" -l
```

For each test file found, add `BuildSignedPaymentBytes` to the mock struct:

```go
func (m *mockSigner) BuildSignedPaymentBytes(_ context.Context, _, _ string, _ uint64) ([]byte, error) {
    return []byte("fake-signed-txn-bytes"), nil
}
```

- [ ] **Step 9: Build to confirm no compile errors**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 10: Run the new test**

```bash
cd backend && go test ./internal/engine/nodes/... -run TestExecuteTool402_AlgorandV2Header -v
```

Expected: `PASS`

- [ ] **Step 11: Run all engine tests**

```bash
cd backend && go test ./internal/engine/... -v
```

Expected: all pass.

- [ ] **Step 12: Commit**

```bash
git add backend/internal/engine/nodes/tool402.go backend/internal/engine/nodes/tool402_v2_test.go
git commit -m "feat: upgrade ExecuteTool402 to x402 v2 X-PAYMENT header for Algorand endpoints"
```

---

## Task 6: Frontend types — add `chainFamily` and `"goplausible"` source

**Files:**
- Modify: `frontend/src/lib/types.ts`

Tiny change but needed by the next two tasks.

- [ ] **Step 1: Update `MarketplaceEndpoint` in `types.ts`**

Open `frontend/src/lib/types.ts`. Find `MarketplaceEndpoint` (around line 100). Replace the `source` and add `chainFamily`:

```typescript
export interface MarketplaceEndpoint {
  id: string;
  name: string;
  description: string;
  provider: string;
  price: string;
  unit: string;
  category: "search" | "data" | "ai" | "finance" | "media" | "util";
  icon?: string;
  tags: string[];
  author?: string;
  calls?: number;
  rating?: number;
  featured?: boolean;
  // Live-sourced fields — absent on static entries
  endpoint?: string;
  discoveredParams?: ParamDef[];
  source?: "static" | "bazaar" | "goplausible";
  chainFamily?: "avm" | "evm" | "svm"; // avm=Algorand, evm=Base/EVM, svm=Solana
}
```

- [ ] **Step 2: Build frontend to confirm no type errors**

```bash
cd frontend && npm run build 2>&1 | grep -E "error|Error" | head -20
```

Expected: no type errors related to `MarketplaceEndpoint`.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/types.ts
git commit -m "feat: add chainFamily and goplausible source to MarketplaceEndpoint type"
```

---

## Task 7: Frontend API — add `goplausibleList` call

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: Add `goplausibleList` to the `marketplace` object in `api.ts`**

Open `frontend/src/lib/api.ts`. Find the `marketplace` export (around line 244). Add `goplausibleList` after `bazaarSearch`:

```typescript
  goplausibleList: async (
    limit = 50,
    offset = 0,
  ): Promise<{ endpoints: MarketplaceEndpoint[] }> => {
    if (BASE) {
      const res = await fetch(
        `${BASE}/marketplace/goplausible?limit=${limit}&offset=${offset}`,
        { credentials: "include" },
      );
      if (!res.ok) throw new Error(`goplausible ${res.status}`);
      return res.json();
    }
    return { endpoints: [] };
  },
```

- [ ] **Step 2: Build to confirm no type errors**

```bash
cd frontend && npm run build 2>&1 | grep -E "error|Error" | head -20
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat: add marketplace.goplausibleList API call"
```

---

## Task 8: Frontend UI — dual marketplace sections with chain badges

**Files:**
- Modify: `frontend/src/components/marketplace/MarketplacePage.tsx`

This is the most visible change. We:
1. Add GoPlausible as a second data source (fetched alongside Bazaar)
2. Create a second section "Algorand Compatible" showing GoPlausible items in a 2-column grid
3. Add a chain badge chip to every card showing `ALGO`, `EVM`, or `SOL`
4. Rename "Live from Bazaar" to "Browse (EVM)" since Coinbase Bazaar items can only be paid with USDC/Base

- [ ] **Step 1: Add GoPlausible state alongside Bazaar state**

Open `frontend/src/components/marketplace/MarketplacePage.tsx`. After the existing Bazaar state declarations (around line 31-33):

```typescript
const [bazaarEndpoints, setBazaarEndpoints] = useState<MarketplaceEndpoint[]>([]);
const [bazaarLoading, setBazaarLoading] = useState(true);
const [bazaarError, setBazaarError] = useState(false);
```

Add GoPlausible state:

```typescript
const [gpEndpoints, setGpEndpoints] = useState<MarketplaceEndpoint[]>([]);
const [gpLoading, setGpLoading] = useState(true);
const [gpError, setGpError] = useState(false);
```

- [ ] **Step 2: Add GoPlausible initial fetch in `useEffect`**

After the Bazaar `useEffect` (which ends around line 47), add a GoPlausible fetch:

```typescript
// GoPlausible (Algorand-native x402) initial load
useEffect(() => {
  setGpLoading(true);
  marketplaceApi
    .goplausibleList(50, 0)
    .then(({ endpoints }) => { setGpEndpoints(endpoints); setGpError(false); })
    .catch(() => setGpError(true))
    .finally(() => setGpLoading(false));
}, []);
```

- [ ] **Step 3: Add GoPlausible category filter**

After the existing `filteredBazaar` memo (around line 77-80), add:

```typescript
const filteredGP = useMemo(() =>
  gpEndpoints.filter((ep) =>
    category === "all" || ep.category === category
  ), [gpEndpoints, category]);
```

- [ ] **Step 4: Update `EndpointCard` to accept and display `chainFamily`**

Find the `EndpointCard` function (around line 208). Update the prop type and add the chain badge.

Replace the function signature:

```typescript
function EndpointCard({ ep, featured = false, onAdd }: { ep: MarketplaceEndpoint; featured?: boolean; onAdd: () => void }) {
```

Inside the card, after the `{ep.source === "bazaar" && <Pill tone="accent">Bazaar</Pill>}` line (around line 221), add a chain badge:

```typescript
{ep.source === "goplausible" && <Pill tone="accent">GoPlausible</Pill>}
{ep.chainFamily === "avm" && (
  <span style={{
    fontSize: 9, fontFamily: "var(--font-mono)", fontWeight: 700,
    background: "rgba(52,211,153,0.15)", border: "1px solid rgba(52,211,153,0.4)",
    color: "#34D399", borderRadius: "var(--r-1)", padding: "1px 6px",
    letterSpacing: "0.04em",
  }}>ALGO</span>
)}
{ep.chainFamily === "evm" && (
  <span style={{
    fontSize: 9, fontFamily: "var(--font-mono)", fontWeight: 700,
    background: "rgba(167,139,250,0.12)", border: "1px solid rgba(167,139,250,0.3)",
    color: "var(--accent)", borderRadius: "var(--r-1)", padding: "1px 6px",
    letterSpacing: "0.04em",
  }}>EVM</span>
)}
{ep.chainFamily === "svm" && (
  <span style={{
    fontSize: 9, fontFamily: "var(--font-mono)", fontWeight: 700,
    background: "rgba(156,163,175,0.12)", border: "1px solid rgba(156,163,175,0.3)",
    color: "var(--fg-muted)", borderRadius: "var(--r-1)", padding: "1px 6px",
    letterSpacing: "0.04em",
  }}>SOL</span>
)}
```

- [ ] **Step 5: Add GoPlausible section to the page render**

Find the `{/* Live Bazaar section */}` block (around line 147). Replace its `<SectionLabel>` from `"Live from Bazaar"` to `"Browse (EVM)"` to make clear these need USDC:

```typescript
<SectionLabel>{query ? `Bazaar results · ${filteredBazaar.length}` : "Browse — EVM (USDC on Base)"}</SectionLabel>
```

Then, **after** the entire existing Bazaar section `</div>` (around line 170), add the GoPlausible section:

```typescript
{/* GoPlausible — Algorand-native x402 section */}
<div style={{ marginBottom: 36 }}>
  <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
    <SectionLabel>
      {gpEndpoints.length > 0
        ? `Algorand Compatible · ${filteredGP.length}`
        : "Algorand Compatible (GoPlausible)"}
    </SectionLabel>
    {gpLoading && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>fetching…</span>}
    {gpError && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "#f87171" }}>goplausible unreachable</span>}
    <span style={{
      fontSize: 9, fontFamily: "var(--font-mono)", fontWeight: 700,
      background: "rgba(52,211,153,0.15)", border: "1px solid rgba(52,211,153,0.4)",
      color: "#34D399", borderRadius: 999, padding: "2px 8px",
    }}>Pay with ALGO</span>
  </div>
  {gpLoading && (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
      {Array.from({ length: 2 }).map((_, i) => <SkeletonCard key={i} />)}
    </div>
  )}
  {!gpLoading && filteredGP.length === 0 && !gpError && (
    <div style={{
      fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)",
      padding: "20px 0", lineHeight: 1.8,
    }}>
      No Algorand-compatible endpoints yet — GoPlausible catalog is new and growing.
      <br />
      <span style={{ opacity: 0.6 }}>Endpoints registered with the GoPlausible Algorand facilitator will appear here.</span>
    </div>
  )}
  {!gpLoading && filteredGP.length > 0 && (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
      {filteredGP.map((ep) => (
        <EndpointCard key={ep.id} ep={ep} onAdd={() => handleAdd(ep)} />
      ))}
    </div>
  )}
</div>
```

- [ ] **Step 6: Start the dev server and verify visually**

```bash
# Make sure backend is running first
cd frontend && npm run dev
```

Open `http://localhost:3000/marketplace` and verify:
- "Browse — EVM (USDC on Base)" section shows Coinbase Bazaar items with purple EVM badges
- "Algorand Compatible (GoPlausible)" section shows below with green ALGO badge in header
- Empty state message shows (since catalog has 0 items)
- No console errors

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/marketplace/MarketplacePage.tsx
git commit -m "feat: dual marketplace UI — EVM Bazaar + Algorand GoPlausible sections with chain badges"
```

---

## Task 9: Final integration smoke test and cleanup

**Files:**
- No file changes — this task is verification only.

- [ ] **Step 1: Run the full backend test suite**

```bash
cd backend && go test ./... 2>&1
```

Expected: all tests pass. No compilation errors.

- [ ] **Step 2: Build the frontend**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: `✓ Compiled successfully` or equivalent Next.js success message. No TypeScript errors.

- [ ] **Step 3: Verify GoPlausible route is live**

```bash
curl -s http://localhost:8080/marketplace/goplausible | python3 -m json.tool
```

Expected:
```json
{
  "endpoints": []
}
```
(Empty because GoPlausible catalog is new. Will fill as merchants register.)

- [ ] **Step 4: Verify GoPlausible facilitator health**

```bash
curl -s https://facilitator.goplausible.xyz/health | python3 -m json.tool
```

Expected: `{"status": "ok"}` or similar.

- [ ] **Step 5: Verify Bazaar items now carry `chainFamily: "evm"`**

```bash
curl -s "http://localhost:8080/marketplace/bazaar?limit=2" | python3 -c "
import json,sys
d=json.load(sys.stdin)
for ep in d['endpoints']:
    print(ep['name'], '→ chainFamily:', ep.get('chainFamily'))
"
```

Expected: all items print `chainFamily: evm`.

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "chore: GoPlausible integration complete — dual marketplace + x402 v2 Algorand payment header"
```

---

## Self-Review

### Spec coverage check

| Requirement | Covered by |
|---|---|
| GoPlausible catalog browsing | Task 2 (`goplausible.go`) + Task 3 (router) |
| Merchant name enrichment | Task 2 (`fetchGPMerchants` join) |
| `chainFamily` field on all endpoints | Task 1 (Bazaar = evm), Task 2 (GoPlausible = avm) |
| `BuildSignedPaymentBytes` without broadcast | Task 4 |
| x402 v2 `X-PAYMENT` header for Algorand | Task 5 |
| Legacy `X-Payment-Txid` path preserved | Task 5 (else branch in ExecuteTool402) |
| `MarketplaceEndpoint` type updated | Task 6 |
| `goplausibleList` API call | Task 7 |
| Dual UI sections | Task 8 |
| ALGO/EVM/SOL chain badges on cards | Task 8 |
| Empty state message for GoPlausible | Task 8 |
| Full test suite passes | Task 9 |

### Placeholder scan — none found ✓

### Type consistency check

- `BazaarEndpoint.ChainFamily` (Go) ↔ `MarketplaceEndpoint.chainFamily` (TS) — both strings, consistent.
- `gpFormatPrice` uses `1e6` divisor — consistent with `formatBazaarPrice` in `marketplace.go`.
- `WalletSigner` interface in `tool402.go` matches `*wallet.Service` which implements both methods after Task 4.
- `x402V2Payload` and `x402AlgorandInner` are local to `tool402.go` — no cross-file type dependency.
- `mockV2Signer` in `tool402_v2_test.go` implements both `WalletSigner` methods — consistent with expanded interface.

### Known limitations (out of scope for this plan)

1. **USDC ASA payments**: GoPlausible Algorand endpoints use USDC as an ASA (Algorand Standard Asset), not native ALGO. `BuildSignedPaymentBytes` currently creates a native ALGO payment (`MakePaymentTxn`). When real GoPlausible items appear requiring USDC-ASA, we'll need `MakeAssetTransferTxn` with the correct ASA ID. This is a separate plan once we have live GoPlausible endpoints to test against.

2. **GoPlausible atomic transaction group**: The full x402 v2 Algorand spec uses a 2-txn atomic group (client payment + facilitator fee txn). Our `BuildSignedPaymentBytes` produces a single signed txn. The facilitator may accept this or may require the full group — verify with live endpoints.

3. **GoPlausible catalog is currently empty**: The integration is production-ready infrastructure; the catalog will fill as Algorand developers register their x402 APIs. The UI correctly shows an empty state message.

---

Sources:
- [GoPlausible x402 Algorand Integration](https://x402.goplausible.xyz/)
- [GoPlausible Facilitator API Docs](https://facilitator.goplausible.xyz/docs)
- [bazaar package (Go) — GoPlausible](https://pkg.go.dev/github.com/GoPlausible/x402-avm/go/extensions/bazaar)
- [x402 Extensions TypeScript examples — GoPlausible GitHub](https://github.com/GoPlausible/.github/blob/main/profile/algorand-x402-documentation/typescript/x402-avm-extensions-examples.md)
