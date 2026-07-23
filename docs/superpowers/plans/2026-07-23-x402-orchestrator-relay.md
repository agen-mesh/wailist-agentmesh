# x402 Orchestrator Relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make AgentMesh a real x402 Orchestrator-entry participant in the Algorand x402 Global Challenge — settle an inbound payment through the GoPlausible facilitator, then pay a downstream x402 endpoint from a platform-controlled wallet, both legs real and mainnet-settled, with zero change to the existing tool402 canvas node or UI.

**Architecture:** A new internal relay (`POST/GET /x402/relay?target=<url>`) sits between the agent's wallet and any external x402 endpoint. Inbound: agent pays the relay's own v2/USDC 402 challenge; relay verifies+settles via the GoPlausible facilitator (credited to us). Outbound: relay pays the real target from a new platform treasury wallet, also via facilitator (credited to them), then relays the target's paid response back. `tool402.go` routes any target whose 402 quote has an `accepts[]` array (real x402 v2) through this relay; the existing flat-quote legacy dialect is untouched and bypasses the relay entirely.

**Tech Stack:** Go 1.x, `github.com/algorand/go-algorand-sdk/v2` v2.11.1, chi router, pgx/golang-migrate, existing `internal/wallet`, `internal/engine/nodes` packages.

## Global Constraints

- Facilitator: `https://facilitator.goplausible.xyz` only — "verified through the GoPlausible facilitator (not any other or local facilitator)". Env-configurable (`FACILITATOR_URL`), default this value.
- Challenge tag: `x402-global-challenge`, placed at `accepts[].extra.tag` in our 402 response (confirmed from live `GET /discovery/resources` catalog data, not just docs).
- Networks (CAIP-2): mainnet `algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=`, testnet `algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=`.
- USDC ASA id: mainnet `31566704`, testnet `10458941`. Always `decimals: 6` in `extra`.
- Facilitator fee-payer address (from live `GET /supported`): `ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA` — this is what goes in `accepts[].extra.feePayer`, and it's the account whose signature the facilitator adds to the fee-stub txn during `/settle`.
- **Testnet does not count toward the leaderboard — mainnet only.** Testnet is validation-only, run once, before flipping `ALGORAND_NETWORK=mainnet`.
- `payTo` (the platform wallet address) must stay the same address for the entire competition — it is the leaderboard's attribution key. Generate it once, store it, never regenerate.
- Downstream targets do **not** need the challenge tag themselves — any real GoPlausible-settled endpoint (tagged or not) counts as real activity credited to them. Confirmed: 662 live catalog resources today, most untagged.
- No refund/escrow mechanics — x402 has no chargeback primitive; if the outbound leg to a target fails after the inbound leg settled, surface an error to the workflow, do not attempt to reverse the inbound settlement.
- Existing tool402 canvas node, its fields, and its legacy (flat `price`/`recipient`, non-`accepts[]`) direct-pay path are unchanged and continue to work exactly as today for non-GoPlausible endpoints.

---

### Task 1: X402 relay settlement ledger (DB)

**Files:**
- Create: `backend/internal/db/migrations/000008_x402_relay_settlements.up.sql`
- Create: `backend/internal/db/migrations/000008_x402_relay_settlements.down.sql`
- Modify: `backend/internal/models/types.go` (add `X402RelaySettlement` type)
- Modify: `backend/internal/db/store.go` (add ledger methods)
- Test: `backend/internal/db/x402_relay_test.go`

**Interfaces:**
- Produces: `models.X402RelaySettlement{ID, TargetURL, InboundTxID, OutboundTxID, AmountAssetMicros int64, Status string, CreatedAt time.Time}`
- Produces: `Store.RecordInboundSettlement(ctx, targetURL, inboundTxID string, amountAssetMicros int64) (models.X402RelaySettlement, error)` — inserts a `pending_outbound` row; returns `db.ErrDuplicateSettlement` (new sentinel) if `inboundTxID` already exists (replay protection).
- Produces: `Store.RecordOutboundSettlement(ctx, id, outboundTxID, status string) error` — updates the row to `status` (`"settled"` or `"failed"`) and sets `outbound_tx_id`.
- Consumes: nothing from other tasks (this is the foundation).

- [ ] **Step 1: Write the migration up/down files**

`backend/internal/db/migrations/000008_x402_relay_settlements.up.sql`:
```sql
CREATE TABLE x402_relay_settlements (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_url           TEXT NOT NULL,
    inbound_tx_id        TEXT NOT NULL UNIQUE,
    outbound_tx_id       TEXT,
    amount_asset_micros  BIGINT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending_outbound'
                         CHECK (status IN ('pending_outbound', 'settled', 'failed')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_x402_relay_settlements_status ON x402_relay_settlements(status);
```

`backend/internal/db/migrations/000008_x402_relay_settlements.down.sql`:
```sql
DROP TABLE IF EXISTS x402_relay_settlements;
```

- [ ] **Step 2: Add the model type**

In `backend/internal/models/types.go`, append:
```go
type X402RelaySettlement struct {
	ID                string    `json:"id"`
	TargetURL         string    `json:"targetUrl"`
	InboundTxID       string    `json:"inboundTxId"`
	OutboundTxID      *string   `json:"outboundTxId,omitempty"`
	AmountAssetMicros int64     `json:"amountAssetMicros"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
}
```

- [ ] **Step 3: Write the failing test**

`backend/internal/db/x402_relay_test.go`:
```go
package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/db"
)

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestRecordInboundSettlementThenOutbound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-1", 100000)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending_outbound" {
		t.Fatalf("want pending_outbound, got %s", row.Status)
	}

	if err := store.RecordOutboundSettlement(ctx, row.ID, "OUTBOUND-TX-1", "settled"); err != nil {
		t.Fatal(err)
	}
}

func TestRecordInboundSettlementRejectsReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-REPLAY", 100000); err != nil {
		t.Fatal(err)
	}
	_, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-REPLAY", 100000)
	if err != db.ErrDuplicateSettlement {
		t.Fatalf("want ErrDuplicateSettlement, got %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./internal/db/... -run TestRecordInboundSettlement -v`
Expected: FAIL — `store.RecordInboundSettlement undefined`

- [ ] **Step 5: Implement the store methods**

In `backend/internal/db/store.go`, add near the credit ledger methods:
```go
// ErrDuplicateSettlement is returned when an inbound settlement's txid has already
// been recorded — a replayed X-PAYMENT payload must never be processed twice.
var ErrDuplicateSettlement = errors.New("duplicate settlement txid")

func (s *Store) RecordInboundSettlement(ctx context.Context, targetURL, inboundTxID string, amountAssetMicros int64) (models.X402RelaySettlement, error) {
	var row models.X402RelaySettlement
	err := s.pool.QueryRow(ctx, `
		INSERT INTO x402_relay_settlements (target_url, inbound_tx_id, amount_asset_micros)
		VALUES ($1, $2, $3)
		RETURNING id, target_url, inbound_tx_id, outbound_tx_id, amount_asset_micros, status, created_at
	`, targetURL, inboundTxID, amountAssetMicros).Scan(
		&row.ID, &row.TargetURL, &row.InboundTxID, &row.OutboundTxID, &row.AmountAssetMicros, &row.Status, &row.CreatedAt,
	)
	if err != nil && strings.Contains(err.Error(), "duplicate key value") {
		return models.X402RelaySettlement{}, ErrDuplicateSettlement
	}
	return row, err
}

func (s *Store) RecordOutboundSettlement(ctx context.Context, id, outboundTxID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE x402_relay_settlements SET outbound_tx_id = $2, status = $3 WHERE id = $1
	`, id, outboundTxID, status)
	return err
}
```
Add `"strings"` to the existing import block in `store.go` if not already present (check first — `store.go` currently imports `context`, `encoding/json`, `errors`, `math`, `time`, `models`, `uuid`, `pgx`, `pgxpool`; `strings` is not yet imported).

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./internal/db/... -run TestRecordInboundSettlement -v`
Expected: PASS (both tests)

- [ ] **Step 7: Commit**

```bash
git add backend/internal/db/migrations/000008_x402_relay_settlements.up.sql \
        backend/internal/db/migrations/000008_x402_relay_settlements.down.sql \
        backend/internal/models/types.go backend/internal/db/store.go \
        backend/internal/db/x402_relay_test.go
git commit -m "db: add x402 relay settlement ledger with replay protection"
```

---

### Task 2: GoPlausible facilitator HTTP client

**Files:**
- Create: `backend/internal/x402/facilitator.go`
- Test: `backend/internal/x402/facilitator_test.go`

**Interfaces:**
- Produces:
  ```go
  package x402

  type FacilitatorClient struct { /* unexported */ }
  func NewFacilitatorClient(baseURL string) *FacilitatorClient

  type PaymentPayload struct {
  	X402Version int            `json:"x402Version"`
  	Scheme      string         `json:"scheme"`
  	Network     string         `json:"network"`
  	Payload     PaymentGroup   `json:"payload"`
  }
  type PaymentGroup struct {
  	PaymentGroup []string `json:"paymentGroup"` // base64 msgpack txns, in group order
  	PaymentIndex int      `json:"paymentIndex"`  // index of the real payment txn
  }
  type PaymentRequirements struct {
  	Scheme            string         `json:"scheme"`
  	Network           string         `json:"network"`
  	MaxAmountRequired string         `json:"maxAmountRequired"`
  	Resource          string         `json:"resource"`
  	Description       string         `json:"description"`
  	MimeType          string         `json:"mimeType"`
  	PayTo             string         `json:"payTo"`
  	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
  	Asset             string         `json:"asset"`
  	Extra             map[string]any `json:"extra"`
  }

  func (c *FacilitatorClient) Verify(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (VerifyResult, error)
  func (c *FacilitatorClient) Settle(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (SettleResult, error)

  type VerifyResult struct {
  	IsValid bool   `json:"isValid"`
  	Invalid string `json:"invalidReason,omitempty"`
  }
  type SettleResult struct {
  	Success bool   `json:"success"`
  	TxID    string `json:"transaction,omitempty"`
  	Error   string `json:"errorReason,omitempty"`
  }
  ```
- Consumes: nothing from other tasks — standalone package.

- [ ] **Step 1: Write the failing test**

`backend/internal/x402/facilitator_test.go`:
```go
package x402_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/x402"
)

func TestVerifySendsPayloadAndParsesResult(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["paymentPayload"] == nil || body["paymentRequirements"] == nil {
			t.Errorf("want both paymentPayload and paymentRequirements in body, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{"isValid": true})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Verify(context.Background(),
		x402.PaymentPayload{X402Version: 2, Scheme: "exact", Network: "algorand:testnet"},
		x402.PaymentRequirements{Scheme: "exact", Network: "algorand:testnet", PayTo: "ADDR"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsValid {
		t.Fatal("want IsValid true")
	}
	if gotPath != "/verify" {
		t.Fatalf("want POST /verify, got %s", gotPath)
	}
}

func TestSettleReturnsTransactionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settle" {
			t.Errorf("want POST /settle, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "TXID123"})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Settle(context.Background(),
		x402.PaymentPayload{X402Version: 2, Scheme: "exact", Network: "algorand:testnet"},
		x402.PaymentRequirements{Scheme: "exact", Network: "algorand:testnet", PayTo: "ADDR"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.TxID != "TXID123" {
		t.Fatalf("want success+TXID123, got %+v", result)
	}
}

func TestSettleSurfacesFacilitatorFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": false, "errorReason": "insufficient_funds"})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Settle(context.Background(), x402.PaymentPayload{}, x402.PaymentRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("want Success false")
	}
	if result.Error != "insufficient_funds" {
		t.Fatalf("want errorReason insufficient_funds, got %q", result.Error)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/x402/... -v`
Expected: FAIL — package `x402` does not exist

- [ ] **Step 3: Implement the facilitator client**

`backend/internal/x402/facilitator.go`:
```go
package x402

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type FacilitatorClient struct {
	baseURL string
	client  *http.Client
}

func NewFacilitatorClient(baseURL string) *FacilitatorClient {
	return &FacilitatorClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

type PaymentGroup struct {
	PaymentGroup []string `json:"paymentGroup"`
	PaymentIndex int      `json:"paymentIndex"`
}

type PaymentPayload struct {
	X402Version int          `json:"x402Version"`
	Scheme      string       `json:"scheme"`
	Network     string       `json:"network"`
	Payload     PaymentGroup `json:"payload"`
}

type PaymentRequirements struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	MaxAmountRequired string         `json:"maxAmountRequired"`
	Resource          string         `json:"resource"`
	Description       string         `json:"description"`
	MimeType          string         `json:"mimeType"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Asset             string         `json:"asset"`
	Extra             map[string]any `json:"extra"`
}

type VerifyResult struct {
	IsValid bool   `json:"isValid"`
	Invalid string `json:"invalidReason,omitempty"`
}

type SettleResult struct {
	Success bool   `json:"success"`
	TxID    string `json:"transaction,omitempty"`
	Error   string `json:"errorReason,omitempty"`
}

func (c *FacilitatorClient) Verify(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (VerifyResult, error) {
	var result VerifyResult
	err := c.post(ctx, "/verify", payload, reqs, &result)
	return result, err
}

func (c *FacilitatorClient) Settle(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (SettleResult, error) {
	var result SettleResult
	err := c.post(ctx, "/settle", payload, reqs, &result)
	return result, err
}

func (c *FacilitatorClient) post(ctx context.Context, path string, payload PaymentPayload, reqs PaymentRequirements, out any) error {
	body, err := json.Marshal(map[string]any{
		"paymentPayload":      payload,
		"paymentRequirements": reqs,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("facilitator %s: server error %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/x402/... -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/x402/facilitator.go backend/internal/x402/facilitator_test.go
git commit -m "x402: add GoPlausible facilitator verify/settle client"
```

---

### Task 3: Wallet — USDC opt-in and atomic-group payment signer

**Files:**
- Modify: `backend/internal/wallet/algorand.go`
- Test: `backend/internal/wallet/usdc_test.go`

**Interfaces:**
- Consumes: `wallet.Service` (existing, `backend/internal/wallet/algorand.go:16`), its existing `encKey`/`algodURL`/`algodToken`/`network` fields and `DecryptMnemonic`.
- Produces:
  ```go
  func (s *Service) OptInAsset(ctx context.Context, encMnemonic string, assetID uint64) (string, error)
  func (s *Service) SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) (paymentGroup []string, paymentIndex int, err error)
  ```
  `SignUSDCPaymentGroup` builds a 2-txn atomic group: txn0 = the caller's signed USDC asset-transfer (`Fee` set to 0 — fee-pooled), txn1 = an **unsigned** payment-stub from `feePayerAddr` to itself for 0 ALGO with `Fee` set to cover both txns (standard Algorand atomic-group fee pooling, the mechanism that makes the payer's leg gasless). Returns both txns base64(msgpack)-encoded in group order, plus which index is the real payment (`0`), matching the documented `paymentGroup`/`paymentIndex` shape used by the facilitator's `PaymentPayload.Payload`.

- [ ] **Step 1: Write the failing test**

`backend/internal/wallet/usdc_test.go`:
```go
package wallet_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/v2/types"

	"github.com/agentmesh/backend/internal/wallet"
)

func testWalletService(t *testing.T) *wallet.Service {
	t.Helper()
	url := os.Getenv("TEST_ALGOD_URL")
	if url == "" {
		t.Skip("TEST_ALGOD_URL not set")
	}
	return wallet.NewService("test-enc-key-32-bytes-long-1234", url, "", "testnet")
}

func TestSignUSDCPaymentGroupProducesTwoTxnsWithCorrectIndex(t *testing.T) {
	svc := testWalletService(t)
	_, encMnemonic, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}

	group, idx, err := svc.SignUSDCPaymentGroup(context.Background(), encMnemonic,
		"LXPC4GQPYH2EZQX2QDYMHCP2I7MXIZMVRPIYTQ3D7R7HXJ4SIHCSYLF5YA",
		10458941, 100000,
		"ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA")
	if err != nil {
		t.Fatal(err)
	}
	if len(group) != 2 {
		t.Fatalf("want 2-txn group, got %d", len(group))
	}
	if idx != 0 {
		t.Fatalf("want paymentIndex 0, got %d", idx)
	}

	// txn0 must decode as a signed asset-transfer with the right amount.
	raw, err := base64.StdEncoding.DecodeString(group[0])
	if err != nil {
		t.Fatal(err)
	}
	var stx types.SignedTxn
	if err := msgpack.Decode(raw, &stx); err != nil {
		t.Fatal(err)
	}
	if stx.Txn.Type != types.AssetTransferTx {
		t.Fatalf("want AssetTransferTx, got %s", stx.Txn.Type)
	}
	if stx.Txn.AssetAmount != 100000 {
		t.Fatalf("want amount 100000, got %d", stx.Txn.AssetAmount)
	}
	if stx.Txn.XferAsset != 10458941 {
		t.Fatalf("want asset 10458941, got %d", stx.Txn.XferAsset)
	}

	// txn1 (fee-payer stub) must decode as unsigned (empty signature).
	raw1, err := base64.StdEncoding.DecodeString(group[1])
	if err != nil {
		t.Fatal(err)
	}
	var unsignedTxn types.Transaction
	if err := msgpack.Decode(raw1, &unsignedTxn); err != nil {
		t.Fatal(err)
	}
	if unsignedTxn.Type != types.PaymentTx {
		t.Fatalf("want fee-payer stub as PaymentTx, got %s", unsignedTxn.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && TEST_ALGOD_URL=https://testnet-api.algonode.cloud go test ./internal/wallet/... -run TestSignUSDCPaymentGroup -v`
Expected: FAIL — `svc.SignUSDCPaymentGroup undefined`

- [ ] **Step 3: Implement `OptInAsset` and `SignUSDCPaymentGroup`**

Append to `backend/internal/wallet/algorand.go` (add `"encoding/base64"` and `"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"` to the import block):
```go
func (s *Service) OptInAsset(ctx context.Context, encMnemonic string, assetID uint64) (string, error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return "", err
	}
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", err
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
	if err != nil {
		return "", err
	}

	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return "", err
	}
	params, err := client.SuggestedParams().Do(ctx)
	if err != nil {
		return "", err
	}
	txn, err := transaction.MakeAssetAcceptanceTxn(acc.Address.String(), nil, params, assetID)
	if err != nil {
		return "", err
	}
	_, signed, err := crypto.SignTransaction(privKey, txn)
	if err != nil {
		return "", err
	}
	return client.SendRawTransaction(signed).Do(ctx)
}

// SignUSDCPaymentGroup builds a 2-txn atomic group for a gasless USDC payment:
// txn0 is the caller's signed asset-transfer (Fee=0, fee-pooled), txn1 is an
// unsigned payment-stub from feePayerAddr to itself that carries both txns'
// fees — the facilitator cosigns txn1 during /settle, so the caller's wallet
// never needs a standing ALGO balance for fees. Returns both txns base64
// (msgpack)-encoded in group order, and which index holds the real payment.
func (s *Service) SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) (paymentGroup []string, paymentIndex int, err error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return nil, 0, err
	}
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return nil, 0, err
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
	if err != nil {
		return nil, 0, err
	}

	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return nil, 0, err
	}
	params, err := client.SuggestedParams().Do(ctx)
	if err != nil {
		return nil, 0, err
	}

	payTxn, err := transaction.MakeAssetTransferTxn(acc.Address.String(), payTo, amountMicros, nil, params, "", assetID)
	if err != nil {
		return nil, 0, err
	}
	payTxn.Fee = 0 // fee-pooled: the stub below covers both txns' fees

	feeStub, err := transaction.MakePaymentTxn(feePayerAddr, feePayerAddr, 0, nil, "", params)
	if err != nil {
		return nil, 0, err
	}
	feeStub.Fee = params.MinFee * 2 // covers this txn + the fee-pooled payment txn

	grouped, err := transaction.AssignGroupID([]types.Transaction{payTxn, feeStub}, "")
	if err != nil {
		return nil, 0, err
	}

	_, signedPay, err := crypto.SignTransaction(privKey, grouped[0])
	if err != nil {
		return nil, 0, err
	}
	unsignedStubBytes := msgpack.Encode(grouped[1])

	return []string{
		base64.StdEncoding.EncodeToString(signedPay),
		base64.StdEncoding.EncodeToString(unsignedStubBytes),
	}, 0, nil
}
```
Also add `"github.com/algorand/go-algorand-sdk/v2/types"` to the import block (needed for `[]types.Transaction`).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && TEST_ALGOD_URL=https://testnet-api.algonode.cloud go test ./internal/wallet/... -run TestSignUSDCPaymentGroup -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/wallet/algorand.go backend/internal/wallet/usdc_test.go
git commit -m "wallet: add USDC opt-in and gasless atomic-group payment signing"
```

---

### Task 4: Platform treasury wallet wiring

**Files:**
- Modify: `backend/internal/api/handlers/deps.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `wallet.Service.OptInAsset` (Task 3), `wallet.Service.GenerateWallet`/`DecryptMnemonic` (existing).
- Produces: `Deps.PlatformWalletAddress string`, `Deps.PlatformWalletEncMnemonic string`, `Deps.FacilitatorClient *x402.FacilitatorClient`, `Deps.USDCAssetID uint64`, `Deps.RelayNetwork string` (CAIP-2 string), `Deps.RelayFeePayer string` — read by Task 5's relay handler.

- [ ] **Step 1: Add fields to `Deps`**

In `backend/internal/api/handlers/deps.go`, add to the `Deps` struct:
```go
	PlatformWalletAddress     string
	PlatformWalletEncMnemonic string
	FacilitatorClient         *x402.FacilitatorClient
	USDCAssetID               uint64
	RelayNetwork              string // CAIP-2, e.g. "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI="
	RelayFeePayer             string // GoPlausible facilitator's signer address for this network
```
Add `"github.com/agentmesh/backend/internal/x402"` to its import block.

- [ ] **Step 2: Wire it in `main.go`**

In `backend/cmd/server/main.go`, after `walletSvc := wallet.NewService(...)`, add:
```go
	platformWalletAddr := os.Getenv("PLATFORM_WALLET_ADDRESS")
	platformWalletEncMnemonic := os.Getenv("PLATFORM_WALLET_ENC_MNEMONIC")
	if platformWalletAddr == "" || platformWalletEncMnemonic == "" {
		log.Fatal("PLATFORM_WALLET_ADDRESS and PLATFORM_WALLET_ENC_MNEMONIC must both be set — the platform wallet's payTo address must stay fixed for the whole competition, so it is provisioned once out-of-band, never auto-generated at startup")
	}

	usdcAssetID := uint64(10458941) // testnet default
	relayNetwork := "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=" // testnet default
	relayFeePayer := "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA"
	if envOr("ALGORAND_NETWORK", "testnet") == "mainnet" {
		usdcAssetID = 31566704
		relayNetwork = "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8="
	}

	facilitatorClient := x402.NewFacilitatorClient(envOr("FACILITATOR_URL", "https://facilitator.goplausible.xyz"))
```
Add `"github.com/agentmesh/backend/internal/x402"` to the import block.

Then in the `deps := &handlers.Deps{...}` literal, add:
```go
		PlatformWalletAddress:     platformWalletAddr,
		PlatformWalletEncMnemonic: platformWalletEncMnemonic,
		FacilitatorClient:         facilitatorClient,
		USDCAssetID:               usdcAssetID,
		RelayNetwork:              relayNetwork,
		RelayFeePayer:             relayFeePayer,
```

- [ ] **Step 3: Verify it builds**

Run: `cd backend && go build ./...`
Expected: builds clean (no test yet — this task is pure wiring, verified by the relay handler's tests in Task 5, which construct `Deps` with these fields directly)

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/handlers/deps.go backend/cmd/server/main.go
git commit -m "config: wire platform treasury wallet and facilitator client"
```

---

### Task 5: X402 relay handler (inbound settle + outbound pay)

**Files:**
- Create: `backend/internal/api/handlers/x402relay.go`
- Modify: `backend/internal/api/router.go`
- Test: `backend/internal/api/handlers/x402relay_test.go`

**Interfaces:**
- Consumes: `Deps.{Store, Wallet, PlatformWalletAddress, PlatformWalletEncMnemonic, FacilitatorClient, USDCAssetID, RelayNetwork, RelayFeePayer}` (Task 4), `Store.RecordInboundSettlement`/`RecordOutboundSettlement` (Task 1), `x402.PaymentPayload`/`PaymentRequirements`/`Verify`/`Settle` (Task 2), `wallet.Service.SignUSDCPaymentGroup` (Task 3).
- Produces: `func (d *Deps) X402Relay(w http.ResponseWriter, r *http.Request)`, registered at `GET/POST /x402/relay`.

- [ ] **Step 1: Write the failing test**

`backend/internal/api/handlers/x402relay_test.go`:
```go
package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/x402"
)

func TestX402RelayNoPaymentMirrorsTargetPriceAsChallengeTag(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"paid response"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"accepts": []map[string]any{{
				"scheme":            "exact",
				"network":           "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
				"maxAmountRequired": "100000",
				"payTo":             "TARGETADDR",
				"asset":             "10458941",
			}},
		})
	}))
	defer target.Close()

	d := &handlers.Deps{
		PlatformWalletAddress: "PLATFORMADDR",
		USDCAssetID:           10458941,
		RelayNetwork:          "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
		RelayFeePayer:         "FEEPAYERADDR",
	}
	req := httptest.NewRequest(http.MethodGet, "/x402/relay?target="+target.URL, nil)
	w := httptest.NewRecorder()

	d.X402Relay(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Accepts []map[string]any `json:"accepts"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Accepts) != 1 {
		t.Fatalf("want 1 accepts entry, got %d", len(body.Accepts))
	}
	extra, _ := body.Accepts[0]["extra"].(map[string]any)
	if extra["tag"] != "x402-global-challenge" {
		t.Fatalf("want tag x402-global-challenge in extra, got %v", extra)
	}
	if body.Accepts[0]["payTo"] != "PLATFORMADDR" {
		t.Fatalf("want payTo=PLATFORMADDR (our own wallet, not the target's), got %v", body.Accepts[0]["payTo"])
	}
	if body.Accepts[0]["maxAmountRequired"] != "100000" {
		t.Fatalf("want price mirrored from target (100000), got %v", body.Accepts[0]["maxAmountRequired"])
	}
	_ = x402.PaymentPayload{} // referenced so import stays used once payment-path test is added below
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/handlers/... -run TestX402RelayNoPaymentMirrorsTargetPriceAsChallengeTag -v`
Expected: FAIL — `d.X402Relay undefined`

- [ ] **Step 3: Implement the relay handler (inbound leg first)**

`backend/internal/api/handlers/x402relay.go`:
```go
package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
	"github.com/agentmesh/backend/internal/x402"
)

const x402RelayHTTPTimeout = 15

// X402Relay is the orchestrator's own paid endpoint. It has no fixed price:
// the price it charges the caller is whatever the target endpoint (given via
// ?target=) actually charges. This is what makes the relay generic across
// every x402 endpoint in the GoPlausible marketplace, not just a fixed set.
//
// Flow: no X-Payment header -> fetch target's real 402, mirror it back as our
// own v2/USDC/tagged challenge (payTo = platform wallet). X-Payment present ->
// verify+settle the inbound payment via the facilitator (credited to us),
// then pay the target from the platform wallet (credited to them), then
// relay the target's paid response back to the caller.
func (d *Deps) X402Relay(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		respond.Error(w, http.StatusBadRequest, "target query param required")
		return
	}

	xPayment := r.Header.Get("X-Payment")
	if xPayment == "" {
		d.relayInboundChallenge(w, r, target)
		return
	}
	d.relaySettleAndForward(w, r, target, xPayment)
}

// relayInboundChallenge fetches the target's real 402 price and mirrors it
// back as our own v2 challenge, tagged for the challenge and paid to our
// platform wallet instead of the target's.
func (d *Deps) relayInboundChallenge(w http.ResponseWriter, r *http.Request, target string) {
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "target fetch failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	var targetChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &targetChallenge); err != nil || len(targetChallenge.Accepts) == 0 {
		respond.Error(w, http.StatusBadGateway, "target did not return a valid x402 challenge")
		return
	}
	targetAccept := targetChallenge.Accepts[0]
	amount, _ := targetAccept["maxAmountRequired"].(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"x402Version": 2,
		"accepts": []map[string]any{{
			"scheme":            "exact",
			"network":           d.RelayNetwork,
			"maxAmountRequired": amount,
			"resource":          target,
			"payTo":             d.PlatformWalletAddress,
			"maxTimeoutSeconds": 300,
			"asset":             strconv.FormatUint(d.USDCAssetID, 10),
			"extra": map[string]any{
				"asset":     strconv.FormatUint(d.USDCAssetID, 10),
				"feePayer":  d.RelayFeePayer,
				"tag":       "x402-global-challenge",
				"decimals":  6,
			},
		}},
	})
}

// relaySettleAndForward verifies+settles the caller's inbound payment, then
// pays the real target from the platform wallet, then relays the target's
// paid response back. Both settlements are real, GoPlausible-facilitated,
// mainnet payments — this is what earns orchestrator-entry attribution.
func (d *Deps) relaySettleAndForward(w http.ResponseWriter, r *http.Request, target, xPaymentHeader string) {
	ctx := r.Context()

	var payload x402.PaymentPayload
	if err := json.Unmarshal([]byte(xPaymentHeader), &payload); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid X-Payment payload")
		return
	}

	reqs := x402.PaymentRequirements{
		Scheme:  "exact",
		Network: d.RelayNetwork,
		PayTo:   d.PlatformWalletAddress,
		Asset:   strconv.FormatUint(d.USDCAssetID, 10),
	}

	verifyResult, err := d.FacilitatorClient.Verify(ctx, payload, reqs)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "facilitator verify failed: "+err.Error())
		return
	}
	if !verifyResult.IsValid {
		respond.Error(w, http.StatusPaymentRequired, "payment invalid: "+verifyResult.Invalid)
		return
	}

	settleResult, err := d.FacilitatorClient.Settle(ctx, payload, reqs)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "facilitator settle failed: "+err.Error())
		return
	}
	if !settleResult.Success {
		respond.Error(w, http.StatusPaymentRequired, "settlement failed: "+settleResult.Error)
		return
	}

	ledgerRow, err := d.Store.RecordInboundSettlement(ctx, target, settleResult.TxID, 0)
	if err == db.ErrDuplicateSettlement {
		respond.Error(w, http.StatusConflict, "payment already processed")
		return
	}
	if err != nil {
		log.Printf("x402 relay: failed to record inbound settlement: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error recording settlement")
		return
	}

	d.payTargetAndRespond(w, r, target, ledgerRow.ID)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/handlers/... -run TestX402RelayNoPaymentMirrorsTargetPriceAsChallengeTag -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for the outbound leg**

Append to `backend/internal/api/handlers/x402relay_test.go`:
```go
type fakeUSDCSigner struct {
	group []string
	idx   int
	err   error
}

func (f *fakeUSDCSigner) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, _ string) ([]string, int, error) {
	return f.group, f.idx, f.err
}

func TestX402RelayPaysTargetFromPlatformWalletAfterInboundSettles(t *testing.T) {
	var targetGotXPayment string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("X-Payment"); h != "" {
			targetGotXPayment = h
			w.Write([]byte(`{"data":"paid response from target"}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"accepts": []map[string]any{{"payTo": "TARGETADDR", "asset": "10458941", "maxAmountRequired": "50000"}},
		})
	}))
	defer target.Close()

	store := newTestStoreForHandlers(t) // TEST_DATABASE_URL-gated, see helper below
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/verify" {
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "INBOUND-TX-" + target.URL})
	}))
	defer facilitator.Close()

	d := &handlers.Deps{
		Store:                     store,
		PlatformWalletAddress:     "PLATFORMADDR",
		PlatformWalletEncMnemonic: "enc-mnemonic",
		FacilitatorClient:         x402.NewFacilitatorClient(facilitator.URL),
		USDCAssetID:               10458941,
		RelayNetwork:              "algorand:testnet",
		RelayFeePayer:             "FEEPAYERADDR",
		USDCSigner:                &fakeUSDCSigner{group: []string{"g0", "g1"}, idx: 0},
	}

	payload, _ := json.Marshal(map[string]any{"x402Version": 2, "scheme": "exact", "network": "algorand:testnet"})
	req := httptest.NewRequest(http.MethodGet, "/x402/relay?target="+target.URL, nil)
	req.Header.Set("X-Payment", string(payload))
	w := httptest.NewRecorder()

	d.X402Relay(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if targetGotXPayment == "" {
		t.Fatal("want relay to have paid the target with its own X-Payment header")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("paid response from target")) {
		t.Fatalf("want target's response relayed back, got %s", w.Body.String())
	}
}
```
Add `"bytes"` and `"context"` to the test file's imports.

- [ ] **Step 6: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/handlers/... -run TestX402RelayPaysTargetFromPlatformWalletAfterInboundSettles -v`
Expected: FAIL — `d.payTargetAndRespond undefined`, `Deps.USDCSigner undefined`, `newTestStoreForHandlers undefined`

- [ ] **Step 7: Add the `USDCSigner` interface, the test store helper, and `payTargetAndRespond`**

In `backend/internal/api/handlers/deps.go`, add the interface and a `Deps` field:
```go
// USDCSigner builds a gasless USDC atomic-payment group for the X-Payment
// header. Satisfied by *wallet.Service (SignUSDCPaymentGroup).
type USDCSigner interface {
	SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) ([]string, int, error)
}
```
Add `USDCSigner USDCSigner` to the `Deps` struct, and `"context"` to its imports. `*wallet.Service` already satisfies this interface after Task 3 — no change needed there.

`backend/internal/db/x402_relay_test.go`'s `newTestStore` helper is package `db_test`; the handlers test needs its own copy since it's a different package. Add to `backend/internal/api/handlers/x402relay_test.go`:
```go
func newTestStoreForHandlers(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}
```
Add `"os"` and `"github.com/agentmesh/backend/internal/db"` to the test file's imports.

Append to `backend/internal/api/handlers/x402relay.go`:
```go
// payTargetAndRespond pays the real target from the platform wallet via the
// facilitator, then relays the target's paid response back to the caller.
// No refund path on failure: x402 has no chargeback primitive, and the
// inbound leg's attribution to us already landed regardless of this outcome.
func (d *Deps) payTargetAndRespond(w http.ResponseWriter, r *http.Request, target, ledgerID string) {
	ctx := r.Context()

	quoteReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	quoteResp, err := http.DefaultClient.Do(quoteReq)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "target unreachable for payment: "+err.Error())
		return
	}
	var targetChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	body, _ := io.ReadAll(io.LimitReader(quoteResp.Body, 1<<20))
	quoteResp.Body.Close()
	if err := json.Unmarshal(body, &targetChallenge); err != nil || len(targetChallenge.Accepts) == 0 {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "target did not return a valid x402 challenge on payment attempt")
		return
	}
	accept := targetChallenge.Accepts[0]
	payTo, _ := accept["payTo"].(string)
	assetStr, _ := accept["asset"].(string)
	amountStr, _ := accept["maxAmountRequired"].(string)
	assetID, _ := strconv.ParseUint(assetStr, 10, 64)
	amount, _ := strconv.ParseUint(amountStr, 10, 64)

	group, idx, err := d.USDCSigner.SignUSDCPaymentGroup(ctx, d.PlatformWalletEncMnemonic, payTo, assetID, amount, d.RelayFeePayer)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusInternalServerError, "failed to sign outbound payment: "+err.Error())
		return
	}
	xPaymentOut, _ := json.Marshal(map[string]any{
		"x402Version": 2, "scheme": "exact", "network": d.RelayNetwork,
		"payload": map[string]any{"paymentGroup": group, "paymentIndex": idx},
	})

	payReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	payReq.Header.Set("X-Payment", string(xPaymentOut))
	payResp, err := http.DefaultClient.Do(payReq)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "paid request to target failed: "+err.Error())
		return
	}
	defer payResp.Body.Close()
	finalBody, _ := io.ReadAll(io.LimitReader(payResp.Body, 5<<20))

	d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "settled")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(finalBody)
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd backend && TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./internal/api/handlers/... -run TestX402Relay -v`
Expected: PASS (both relay tests)

- [ ] **Step 9: Register the route**

In `backend/internal/api/router.go`, add to the public routes section (near `r.Post("/run/{workflowId}", ...)`):
```go
	r.Handle("/x402/relay", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.X402Relay(w, r)
	}))
```

- [ ] **Step 10: Run full package build + tests**

Run: `cd backend && go build ./... && go test ./internal/api/... ./internal/db/... ./internal/wallet/... ./internal/x402/... -v`
Expected: all PASS

- [ ] **Step 11: Commit**

```bash
git add backend/internal/api/handlers/x402relay.go backend/internal/api/handlers/x402relay_test.go \
        backend/internal/api/handlers/deps.go backend/internal/api/router.go
git commit -m "handlers: add x402 orchestrator relay (settle inbound, pay target, relay response)"
```

---

### Task 6: Route real x402 v2 targets through the relay from `tool402.go`

**Files:**
- Modify: `backend/internal/engine/nodes/tool402.go`
- Modify: `backend/internal/engine/runner.go` (pass relay base URL + USDC signer through to `ExecuteTool402`)
- Test: `backend/internal/engine/nodes/tool402_test.go` (extend)

**Interfaces:**
- Consumes: `x402/relay?target=` route (Task 5).
- Produces: `ExecuteTool402` gains two new params: `relayBaseURL string, usdcSigner USDCGroupSigner` (new interface, mirrors `WalletSigner`). Existing call sites (`runner.go`'s `NodeTypeTool402` case) updated to pass them through.

- [ ] **Step 1: Write the failing test — v2 target routes through the relay**

Append to `backend/internal/engine/nodes/tool402_test.go`:
```go
type mockUSDCGroupSigner struct {
	group []string
	idx   int
}

func (m *mockUSDCGroupSigner) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, _ string) ([]string, int, error) {
	return m.group, m.idx, nil
}

// TestX402V2TargetRoutesThroughRelay verifies that a target advertising the
// real x402 v2 shape (accepts[]) is never paid directly — the agent pays the
// relay instead, which is what earns orchestrator-entry attribution.
func TestX402V2TargetRoutesThroughRelay(t *testing.T) {
	var targetHit, relayHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"accepts":[{"scheme":"exact","payTo":"TARGETADDR","asset":"10458941","maxAmountRequired":"100000"}]}`))
	}))
	defer target.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHit = true
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"relayed paid response"}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"accepts":[{"scheme":"exact","payTo":"PLATFORMADDR","asset":"10458941","maxAmountRequired":"100000"}]}`))
	}))
	defer relay.Close()

	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: target.URL}
	rc := engine.NewRunContext("r1", nil)
	aw := models.AgentWallet{AgentNodeID: "a1", EncryptedMnemonic: "enc-mnemonic"}
	signer := &mockSigner{txID: "unused-legacy-path"}
	usdcSigner := &mockUSDCGroupSigner{group: []string{"g0", "g1"}, idx: 0}

	result, err := nodes.ExecuteTool402V2(context.Background(), node, rc, aw, signer, usdcSigner, relay.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !targetHit {
		t.Fatal("want relay to have queried target's real price first")
	}
	if !relayHit {
		t.Fatal("want relay to have been called")
	}
	m, ok := result.(map[string]any)
	if !ok || m["data"] != "relayed paid response" {
		t.Fatalf("want relayed response, got %v", result)
	}
}

// TestX402LegacyTargetBypassesRelay verifies the existing flat-quote dialect
// (no accepts[]) still pays the target directly — unchanged behavior.
func TestX402LegacyTargetBypassesRelay(t *testing.T) {
	var relayHit bool
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayHit = true
	}))
	defer relay.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("X-Payment-Txid"); h != "" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)
	aw := models.AgentWallet{AgentNodeID: "a1", EncryptedMnemonic: "enc-mnemonic"}
	signer := &mockSigner{txID: "TX-SIGNED-123"}
	usdcSigner := &mockUSDCGroupSigner{}

	result, err := nodes.ExecuteTool402V2(context.Background(), node, rc, aw, signer, usdcSigner, relay.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relayHit {
		t.Fatal("legacy target must bypass the relay entirely")
	}
	m := result.(map[string]any)
	if m["txId"] != "TX-SIGNED-123" {
		t.Fatalf("want legacy direct-pay path unchanged, got %v", m)
	}
}
```
Add `"github.com/agentmesh/backend/internal/engine"` if not already imported (it already is, per the existing file).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/nodes/... -run TestX402V2TargetRoutesThroughRelay -v`
Expected: FAIL — `nodes.ExecuteTool402V2 undefined`

- [ ] **Step 3: Implement `ExecuteTool402V2` in `tool402.go`**

Append to `backend/internal/engine/nodes/tool402.go`:
```go
// USDCGroupSigner signs a gasless USDC atomic-payment group for the relay's
// X-Payment header. Satisfied by *wallet.Service (SignUSDCPaymentGroup).
type USDCGroupSigner interface {
	SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) ([]string, int, error)
}

// ExecuteTool402V2 is the entry point runner.go calls for tool402 nodes. It
// inspects the target's 402 quote shape: a real x402 v2 challenge (accepts[])
// is routed through the AgentMesh relay so both payment legs are real,
// GoPlausible-settled, and attributable to us as an orchestrator entry. The
// legacy flat-quote dialect (no accepts[]) bypasses the relay entirely and
// keeps today's direct-pay behavior unchanged — it was never
// GoPlausible-compliant and isn't becoming so.
func ExecuteTool402V2(ctx context.Context, node models.WorkflowNode, rc RunContexter, aw models.AgentWallet, signer WalletSigner, usdcSigner USDCGroupSigner, relayBaseURL string) (any, error) {
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	resp.Body.Close()
	var v2Challenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	if json.Unmarshal(body, &v2Challenge) == nil && len(v2Challenge.Accepts) > 0 {
		return executeTool402V2Relay(ctx, node, aw, usdcSigner, relayBaseURL)
	}

	// Legacy flat-quote dialect: unchanged direct-pay path.
	return ExecuteTool402(ctx, node, rc, aw, signer)
}

func executeTool402V2Relay(ctx context.Context, node models.WorkflowNode, aw models.AgentWallet, usdcSigner USDCGroupSigner, relayBaseURL string) (any, error) {
	if aw.EncryptedMnemonic == "" || usdcSigner == nil {
		return map[string]any{"error": "payment required but no agent wallet configured"}, nil
	}

	relayURL := relayBaseURL + "/x402/relay?target=" + node.Endpoint

	quoteReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, relayURL, nil)
	quoteResp, err := toolHTTPClient.Do(quoteReq)
	if err != nil {
		return nil, fmt.Errorf("x402 relay quote failed: %w", err)
	}
	quoteBody, _ := io.ReadAll(io.LimitReader(quoteResp.Body, httpResponseLimit))
	quoteResp.Body.Close()

	var relayChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	if json.Unmarshal(quoteBody, &relayChallenge) != nil || len(relayChallenge.Accepts) == 0 {
		return nil, fmt.Errorf("x402 relay: invalid challenge response")
	}
	accept := relayChallenge.Accepts[0]
	payTo, _ := accept["payTo"].(string)
	assetStr, _ := accept["asset"].(string)
	amountStr, _ := accept["maxAmountRequired"].(string)
	feePayer, _ := accept["extra"].(map[string]any)["feePayer"].(string)
	assetID, _ := strconv.ParseUint(assetStr, 10, 64)
	amount, _ := strconv.ParseUint(amountStr, 10, 64)

	group, idx, err := usdcSigner.SignUSDCPaymentGroup(ctx, aw.EncryptedMnemonic, payTo, assetID, amount, feePayer)
	if err != nil {
		return nil, fmt.Errorf("x402 relay payment signing failed: %w", err)
	}
	xPayment, _ := json.Marshal(map[string]any{
		"x402Version": 2, "scheme": "exact",
		"payload": map[string]any{"paymentGroup": group, "paymentIndex": idx},
	})

	payReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, relayURL, nil)
	payReq.Header.Set("X-Payment", string(xPayment))
	payResp, err := toolHTTPClient.Do(payReq)
	if err != nil {
		return nil, fmt.Errorf("x402 relay payment request failed: %w", err)
	}
	defer payResp.Body.Close()
	finalBody, _ := io.ReadAll(io.LimitReader(payResp.Body, httpResponseLimit))

	var result any
	if json.Unmarshal(finalBody, &result) == nil {
		return result, nil
	}
	return string(finalBody), nil
}
```
Add `"strconv"` to the existing import block in `tool402.go` (currently: `context`, `encoding/json`, `fmt`, `io`, `net/http`, `strconv`, `models` — `strconv` is already imported per the original file, verify and skip if present).

- [ ] **Step 4: Update `runner.go`'s call site**

In `backend/internal/engine/runner.go`, the existing `case models.NodeTypeTool402:` (around line 282) currently calls `nodes.ExecuteTool402(...)`. Change it to call `nodes.ExecuteTool402V2(...)`, passing the runner's existing wallet-signer field plus two new ones: the runner's `wallet.Service` (already present, since it already implements `WalletSigner` — it now also implements `USDCGroupSigner` after Task 3) and a relay base URL. Add a `RelayBaseURL string` field to `engine.Runner` (mirrors how `BaseURL` is threaded through `Deps` in `main.go`), defaulting to `envOr("BASE_URL", "http://localhost:8080")` — same value already computed in `main.go` for `Deps.BaseURL`, so the runner should receive it via `engine.NewRunner`'s existing constructor call in `main.go`:
```go
	runner := engine.NewRunner(store, broker, walletSvc, envOr("BASE_URL", "http://localhost:8080"))
```
Update `engine.NewRunner`'s signature and the `Runner` struct to store this as `relayBaseURL string`, and update the `NodeTypeTool402` case to:
```go
	case models.NodeTypeTool402:
		return nodes.ExecuteTool402V2(ctx, node, rc, wallet, r.wallet, r.wallet, r.relayBaseURL)
```
(`wallet` here is the existing `models.AgentWallet` value already resolved earlier in that function for the node's agent; `r.wallet` is the runner's existing `*wallet.Service` field, satisfying both `WalletSigner` and `USDCGroupSigner`.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go build ./... && go test ./internal/engine/... -v`
Expected: all PASS, including the two new tests and every pre-existing test in `tool402_test.go` (the legacy `ExecuteTool402` function itself is untouched)

- [ ] **Step 6: Commit**

```bash
git add backend/internal/engine/nodes/tool402.go backend/internal/engine/nodes/tool402_test.go backend/internal/engine/runner.go backend/cmd/server/main.go
git commit -m "engine: route real x402 v2 targets through the orchestrator relay"
```

---

### Task 7: End-to-end integration test (fake facilitator + fake downstream, full round trip)

**Files:**
- Create: `backend/internal/engine/x402_orchestrator_integration_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6. This task adds no new production code — it is a single test proving the whole chain works together, gated on `TEST_DATABASE_URL` like the other DB-backed tests in this repo.

- [ ] **Step 1: Write the integration test**

```go
package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/x402"
)

// TestOrchestratorRoundTripBothLegsSettle exercises the full orchestrator
// flow end-to-end: a fake "agent" pays the relay, the relay settles that
// through a fake facilitator, then pays a fake downstream target through the
// same fake facilitator, and the target's response comes back to the agent.
// This is the shape the real challenge scores: "client pays your endpoint,
// your backend pays the downstream endpoints, and the synthesized paid
// response is returned."
func TestOrchestratorRoundTripBothLegsSettle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var facilitatorCalls []string
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facilitatorCalls = append(facilitatorCalls, r.URL.Path)
		if r.URL.Path == "/verify" {
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "settled-" + string(rune(len(facilitatorCalls)))})
	}))
	defer facilitator.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"real downstream result"}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"accepts":[{"scheme":"exact","payTo":"TARGETADDR","asset":"10458941","maxAmountRequired":"75000"}]}`))
	}))
	defer target.Close()

	d := &handlers.Deps{
		Store:                     store,
		PlatformWalletAddress:     "PLATFORMADDR",
		PlatformWalletEncMnemonic: "enc-mnemonic",
		FacilitatorClient:         x402.NewFacilitatorClient(facilitator.URL),
		USDCAssetID:               10458941,
		RelayNetwork:              "algorand:testnet",
		RelayFeePayer:             "FEEPAYERADDR",
		USDCSigner:                &fakeUSDCSignerForIntegration{},
	}

	relay := httptest.NewServer(http.HandlerFunc(d.X402Relay))
	defer relay.Close()

	// Agent's first call: no payment, gets the mirrored 402.
	quoteResp, err := http.Get(relay.URL + "?target=" + target.URL)
	if err != nil {
		t.Fatal(err)
	}
	if quoteResp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("want 402 on first call, got %d", quoteResp.StatusCode)
	}

	// Agent's second call: with payment, should settle both legs and return
	// the target's real data.
	payload, _ := json.Marshal(map[string]any{"x402Version": 2, "scheme": "exact", "network": "algorand:testnet"})
	payReq, _ := http.NewRequest(http.MethodGet, relay.URL+"?target="+target.URL, nil)
	payReq.Header.Set("X-Payment", string(payload))
	payResp, err := http.DefaultClient.Do(payReq)
	if err != nil {
		t.Fatal(err)
	}
	defer payResp.Body.Close()
	var final map[string]any
	json.NewDecoder(payResp.Body).Decode(&final)
	if final["data"] != "real downstream result" {
		t.Fatalf("want downstream data relayed back, got %v", final)
	}

	if len(facilitatorCalls) != 2 {
		t.Fatalf("want exactly 2 facilitator calls (verify+settle for the inbound leg), got %d: %v", len(facilitatorCalls), facilitatorCalls)
	}
}

type fakeUSDCSignerForIntegration struct{}

func (f *fakeUSDCSignerForIntegration) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, _ string) ([]string, int, error) {
	return []string{"g0", "g1"}, 0, nil
}
```

- [ ] **Step 2: Run it**

Run: `cd backend && TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./internal/engine/... -run TestOrchestratorRoundTripBothLegsSettle -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add backend/internal/engine/x402_orchestrator_integration_test.go
git commit -m "test: add end-to-end orchestrator round-trip integration test"
```

---

### Task 8: Testnet validation, then mainnet cutover (runbook)

**Files:** none (operational steps + env changes only)

This task is the explicit checkpoint for the one thing this plan cannot verify by unit test alone: whether the exact atomic-group wire format built in Task 3 is byte-for-byte what the real GoPlausible facilitator expects. The fee-pooling mechanism (payer's txn `Fee=0`, stub txn `Fee=2×MinFee`) is standard Algorand protocol behavior, not GoPlausible-specific, but the facilitator's exact tolerance for the message layout has not been exercised against a live facilitator anywhere in Tasks 1–7 — all of those tests use fakes. Per the challenge's own rules, testnet is validation-only and doesn't count toward the leaderboard, so this is exactly where that's supposed to happen.

- [ ] **Step 1: Provision a testnet platform wallet**

```bash
cd backend
go run ./cmd/genwallet  # or a one-off script using wallet.Service.GenerateWallet — if no such cmd exists yet, write a 5-line throwaway main.go, run once, discard
```
Fund it with testnet ALGO via `wallet.Service.FundFromDispenser` (existing method) for opt-in MBR.

- [ ] **Step 2: Opt the testnet platform wallet into testnet USDC**

Call `wallet.Service.OptInAsset(ctx, encMnemonic, 10458941)` once (e.g. via a throwaway test or REPL-style `go run`). Confirm on `https://lora.algokit.io/testnet/account/<address>` that the USDC ASA now shows a zero balance (opted in).

- [ ] **Step 3: Set testnet env vars and run the server**

```bash
export ALGOD_URL=https://testnet-api.algonode.cloud
export ALGORAND_NETWORK=testnet
export FACILITATOR_URL=https://facilitator.goplausible.xyz
export PLATFORM_WALLET_ADDRESS=<address from step 1>
export PLATFORM_WALLET_ENC_MNEMONIC=<enc mnemonic from step 1>
go run ./cmd/server
```

- [ ] **Step 4: Fund one real agent wallet with testnet USDC, deploy a workflow with a tool402 node pointed at a real catalog endpoint**

Pick a live testnet entry from `curl -s https://facilitator.goplausible.xyz/discovery/resources | jq '.items[] | select(.accepts[0].network | contains("SGO1GK"))'` (testnet CAIP-2 prefix). Opt the agent wallet into testnet USDC (`OptInAsset`) and fund it via the testnet USDC faucet or a manual transfer.

- [ ] **Step 5: Trigger the workflow, confirm both settlements land**

Run the workflow; watch server logs for two facilitator `/settle` calls. Confirm via `curl https://facilitator.goplausible.xyz/discovery/resources?resourceUrl=<our-relay-url>` (or the equivalent filter) that our relay's `settleCount` incremented, and check the real downstream target's `settleCount` incremented too.

- [ ] **Step 6: If anything about the wire format was wrong, fix it here — this is the checkpoint**

If `/verify` or `/settle` reject the payload, the facilitator's error message names the problem (malformed group, wrong fee, bad group ID, etc.) — fix `SignUSDCPaymentGroup` (Task 3) accordingly and re-run Steps 3–5. Do not proceed to Step 7 until one full real testnet round-trip succeeds.

- [ ] **Step 7: Cut over to mainnet**

Provision a **separate** mainnet platform wallet (Step 1–2 repeated with `ALGOD_URL=https://mainnet-api.algonode.cloud`, `ALGORAND_NETWORK=mainnet`, USDC ASA `31566704`). This address is now fixed for the rest of the competition — do not regenerate it. Update production env vars accordingly and redeploy.

- [ ] **Step 8: Confirm one real mainnet settlement, then leave it running**

Repeat Steps 4–5 against mainnet with a small real amount. Once confirmed, the relay is live and accumulating leaderboard-counted activity on every subsequent tool402 call against any real x402 v2 target.

---

## Self-Review Notes

- **Spec coverage:** facilitator client (Task 2), USDC atomic-group signing (Task 3), platform wallet (Task 4), relay inbound+outbound (Task 5), tool402 dual-protocol routing (Task 6), replay protection (Task 1), end-to-end proof (Task 7), testnet-then-mainnet cutover per the blog's explicit rule (Task 8) — every spec section has a task.
- **Type consistency checked:** `USDCGroupSigner`/`USDCSigner` interface signature `(ctx, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) ([]string, int, error)` is identical across Task 3's production method, Task 5's `Deps.USDCSigner` field, Task 6's `ExecuteTool402V2` param, and every mock/fake in Tasks 5–7.
- **No refund/escrow, no canvas/UI change** — confirmed absent from every task, matching the spec's explicit out-of-scope list.
