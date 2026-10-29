package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/purchasesim"
	worldsim "github.com/agentmesh/backend/internal/world/simulation"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/google/uuid"
)

func controlJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func controlBody(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "JSON POST required", 400)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		http.Error(w, "invalid control input", 400)
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		http.Error(w, "invalid control input", 400)
		return false
	}
	return true
}
func (a *simulationApp) controls() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "loopback controls only", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if origin != a.frontend {
				http.Error(w, "origin rejected", 403)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		switch r.URL.Path {
		case "/__verify__/capabilities":
			caps := a.fixture.Capabilities()
			for _, outcome := range worldsim.SupportedOutcomes() {
				caps.Features["world."+string(outcome)] = true
			}
			for _, feature := range []string{"world.success", "world.denied", "world.stale", "merchant.none", "merchant.execute_error", "merchant.claim_lost", "merchant.finish_before", "merchant.finish_lost", "merchant.lookup_unavailable", "external_relayer", "dryrun"} {
				caps.Features[feature] = true
			}
			caps.Features["app_restart"] = a.restart != nil
			caps.Features["restart"] = a.restart != nil
			controlJSON(w, map[string]any{"evidenceMode": "simulation", "features": caps.Features, "offerKey": a.fixture.OfferKey(), "merchantResource": simulationResource, "priceMicros": "10000", "controlsVersion": "1", "scenarioStatePolicy": "preserve-chain-and-journal", "restartStatePolicy": "preserve-chain-database-and-private-context", "worldSubjectPolicy": "scenario subject retained across fault changes"})
		case "/__verify__/restart":
			var body struct {
				PreserveDatabase bool `json:"preserveDatabase"`
			}
			if !controlBody(w, r, &body) {
				return
			}
			if !body.PreserveDatabase || a.restart == nil {
				http.Error(w, "restart requires preserved scratch state", 400)
				return
			}
			if err := a.mutateFixture(func() error { return nil }); err != nil {
				http.Error(w, "private state unavailable", 503)
				return
			}
			controlJSON(w, map[string]bool{"restarting": true})
			select {
			case a.restart <- struct{}{}:
			default:
			}
		case "/__verify__/scenario", "/__verify__/configure":
			var body struct {
				ID     string                                       `json:"id"`
				Reset  *bool                                        `json:"reset"`
				Faults struct{ World, Arkiv, RPC, Merchant string } `json:"faults"`
			}
			if !controlBody(w, r, &body) {
				return
			}
			outcome := worldsim.Outcome(body.Faults.World)
			if outcome == "" || outcome == "none" {
				outcome = worldsim.Success
			}
			switch body.Faults.Merchant {
			case "", "none", "execute_error", "claim_lost", "finish_before", "finish_lost", "lookup_unavailable":
			default:
				http.Error(w, "unsupported merchant fault", 400)
				return
			}
			if !slices.Contains(worldsim.SupportedOutcomes(), outcome) {
				http.Error(w, "unsupported World fault", 400)
				return
			}
			if err = a.mutateFixture(func() error {
				return a.fixture.SetFaults(purchasesim.Faults{RPC: body.Faults.RPC, Arkiv: body.Faults.Arkiv})
			}); err != nil {
				http.Error(w, "fixture change unavailable", 503)
				return
			}
			if err = a.issuer.SetOutcome(outcome); err != nil {
				http.Error(w, "unsupported identity fixture fault", 400)
				return
			}
			a.mu.Lock()
			a.merchantFault = body.Faults.Merchant
			reset := r.URL.Path == "/__verify__/scenario"
			if body.Reset != nil {
				reset = *body.Reset
			}
			if reset {
				a.counters = map[string]int{}
				a.state.Subject = "scenario-" + uuid.NewString()
				if err = a.persistState(); err != nil {
					a.mu.Unlock()
					http.Error(w, "private subject unavailable", 503)
					return
				}
				if err = a.issuer.SetSubject(a.state.Subject); err != nil {
					a.mu.Unlock()
					http.Error(w, "subject unavailable", 503)
					return
				}
			}
			a.mu.Unlock()
			controlJSON(w, map[string]any{"configured": true, "evidenceMode": "simulation"})
		case "/__verify__/observations":
			a.observations(w, r)
		case "/__verify__/wallet":
			a.walletControl(w, r)
		case "/__verify__/advance":
			a.advance(w, r)
		case "/__verify__/dryrun":
			var body struct {
				WorkflowID string `json:"workflowId"`
			}
			if !controlBody(w, r, &body) {
				return
			}
			wf, err := a.store.GetWorkflow(r.Context(), body.WorkflowID)
			if err != nil {
				http.Error(w, "workflow not found", 404)
				return
			}
			for _, node := range wf.Nodes {
				if node.Type != models.NodeTypeTrigger && node.Type != models.NodeTypePurchase && node.Type != models.NodeTypeEnd {
					http.Error(w, "fixture dryrun permits trigger, purchase, end only", 400)
					return
				}
			}
			result := engine.DryRun(r.Context(), models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges}, engine.DryRunOptions{Input: "controlled dry run"})
			controlJSON(w, result)
		case "/__verify__/arc":
			a.fixtureHTTP(a.fixture.ArcHandler()).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (a *simulationApp) observations(w http.ResponseWriter, r *http.Request) {
	obs := a.fixture.Observations()
	a.mu.Lock()
	counts := map[string]int{"merchant.execute": 0, "merchant.lookup": 0, "merchant.quote": 0, "merchant.accept": 0, "merchant.fulfill": 0, "world.token": 0}
	for k, v := range a.counters {
		counts[k] = v
	}
	a.mu.Unlock()
	counts["rpc.broadcast"] = obs.SendAttempts
	counts["rpc.external"] = obs.ExternalAttempts
	counts["arkiv.query"] = obs.ArkivQueries
	var orders, holds, releases int
	if err := a.pool.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER (WHERE payload ? 'signatureReleasedAt') FROM purchase_orders`).Scan(&orders, &releases); err != nil {
		http.Error(w, "observation unavailable", 503)
		return
	}
	if err := a.pool.QueryRow(r.Context(), `SELECT count(*) FROM purchase_allowances WHERE reserved_micros>0`).Scan(&holds); err != nil {
		http.Error(w, "observation unavailable", 503)
		return
	}
	counts["purchase.orders"] = orders
	counts["purchase.holds"] = holds
	counts["purchase.signature_releases"] = releases
	counts["purchase.signature_release"] = releases
	var runsJSON []byte
	if err := a.pool.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'status',status)),'[]'::jsonb) FROM runs`).Scan(&runsJSON); err != nil {
		http.Error(w, "observation unavailable", 503)
		return
	}
	var orderJSON []byte
	if err := a.pool.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'state',state,'txHash',payload->>'txHash','intentHash',intent_hash,'paymentEvent',payload->'paymentEvent')),'[]'::jsonb) FROM purchase_orders`).Scan(&orderJSON); err != nil {
		http.Error(w, "observation unavailable", 503)
		return
	}
	controlJSON(w, map[string]any{"evidenceMode": "simulation", "counters": counts, "broadcastHashes": obs.TransactionHashes, "externalTransactionHashes": obs.ExternalTransactionHashes, "uniqueTransactions": obs.UniqueTransactions, "uniqueAuthorizations": obs.UniqueAuthorizations, "consumedAuthorizations": obs.ConsumedAuthorizations, "orders": json.RawMessage(orderJSON), "runs": json.RawMessage(runsJSON)})
}

func (a *simulationApp) walletControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action    string          `json:"action"`
		WalletRef string          `json:"walletRef"`
		Purpose   string          `json:"purpose"`
		Message   string          `json:"message"`
		TypedData json.RawMessage `json:"typedData"`
	}
	if !controlBody(w, r, &body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var key *ecdsa.PrivateKey
	var err error
	if body.Action == "new" {
		if body.Purpose == "provider" {
			key, err = crypto.HexToECDSA(a.state.Provider)
		} else {
			key, err = crypto.GenerateKey()
		}
		if err != nil {
			http.Error(w, "wallet unavailable", 503)
			return
		}
		ref := uuid.NewString()
		a.state.Wallets[ref] = hex.EncodeToString(crypto.FromECDSA(key))
		if err = a.persistState(); err != nil {
			delete(a.state.Wallets, ref)
			http.Error(w, "wallet persistence unavailable", 503)
			return
		}
		controlJSON(w, map[string]string{"walletRef": ref, "address": strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())})
		return
	}
	raw, ok := a.state.Wallets[body.WalletRef]
	if !ok {
		http.Error(w, "unknown fixture wallet", 404)
		return
	}
	key, err = crypto.HexToECDSA(raw)
	if err != nil {
		http.Error(w, "wallet unavailable", 503)
		return
	}
	var digest []byte
	switch body.Action {
	case "sign_personal":
		digest = accounts.TextHash([]byte(body.Message))
	case "sign_typed_data":
		var data apitypes.TypedData
		if json.Unmarshal(body.TypedData, &data) != nil {
			http.Error(w, "invalid typed data", 400)
			return
		}
		digest, _, err = apitypes.TypedDataAndHash(data)
	default:
		http.Error(w, "unsupported wallet action", 400)
		return
	}
	if err != nil {
		http.Error(w, "invalid typed data", 400)
		return
	}
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		http.Error(w, "signature unavailable", 503)
		return
	}
	sig[64] += 27
	controlJSON(w, map[string]string{"signature": "0x" + hex.EncodeToString(sig)})
}

func (a *simulationApp) persistState() error {
	raw, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	return writePrivate(a.stateFile, raw)
}

func (a *simulationApp) advance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind        string          `json:"kind"`
		TypedData   json.RawMessage `json:"typedData"`
		Signature   string          `json:"signature"`
		ArcBlocks   uint64          `json:"arcBlocks"`
		ArkivBlocks uint64          `json:"arkivBlocks"`
	}
	if !controlBody(w, r, &body) {
		return
	}
	if body.Kind == "blocks" {
		if err := a.mutateFixture(func() error { return a.fixture.AdvanceBlocks(body.ArcBlocks, body.ArkivBlocks) }); err != nil {
			http.Error(w, "invalid block advance", 400)
			return
		}
		controlJSON(w, map[string]bool{"advanced": true})
		return
	}
	if body.Kind != "external_relayer" {
		http.Error(w, "unsupported advance", 400)
		return
	}
	var data apitypes.TypedData
	if json.Unmarshal(body.TypedData, &data) != nil || data.PrimaryType != "TransferWithAuthorization" || data.Domain.Name != "USDC" || data.Domain.Version != "2" || data.Domain.ChainId == nil || (*big.Int)(data.Domain.ChainId).Int64() != purchase.ChainID || !strings.EqualFold(data.Domain.VerifyingContract, purchase.Asset) {
		http.Error(w, "invalid authorization domain", 400)
		return
	}
	text := func(k string) string { s, _ := data.Message[k].(string); return s }
	value, vOK := new(big.Int).SetString(text("value"), 10)
	after, aOK := new(big.Int).SetString(text("validAfter"), 10)
	before, bOK := new(big.Int).SetString(text("validBefore"), 10)
	sig, err := hex.DecodeString(strings.TrimPrefix(body.Signature, "0x"))
	if !vOK || !aOK || !bOK || err != nil || len(sig) != 65 || !common.IsHexAddress(text("from")) || !common.IsHexAddress(text("to")) || len(text("nonce")) != 66 {
		http.Error(w, "invalid authorization", 400)
		return
	}
	v := sig[64]
	if v >= 27 {
		v -= 27
	}
	if !crypto.ValidateSignatureValues(v, new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		http.Error(w, "invalid signature", 400)
		return
	}
	auth := arc.Authorization{From: common.HexToAddress(text("from")), To: common.HexToAddress(text("to")), Value: value, ValidAfter: after, ValidBefore: before, Nonce: common.HexToHash(text("nonce")), V: v + 27}
	copy(auth.R[:], sig[:32])
	copy(auth.S[:], sig[32:64])
	recovered, err := arc.RecoverAuthorizationSigner("USDC", "2", purchase.ChainID, common.HexToAddress(purchase.Asset), auth)
	if err != nil || recovered != auth.From {
		http.Error(w, "invalid signature", 400)
		return
	}
	call, err := arc.AuthorizationData(auth)
	if err != nil {
		http.Error(w, "invalid authorization", 400)
		return
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		http.Error(w, "ephemeral relayer unavailable", 503)
		return
	}
	to := common.HexToAddress(purchase.Asset)
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(purchase.ChainID), Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 200000, To: &to, Value: big.NewInt(0), Data: call})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(purchase.ChainID)), key)
	if err != nil {
		http.Error(w, "local transaction unavailable", 503)
		return
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		http.Error(w, "local transaction unavailable", 503)
		return
	}
	var hash string
	err = a.mutateFixture(func() error {
		var submitErr error
		hash, submitErr = a.fixture.SubmitExternal("0x" + hex.EncodeToString(raw))
		return submitErr
	})
	if err != nil {
		http.Error(w, "external fixture relay rejected", 400)
		return
	}
	controlJSON(w, map[string]string{"txHash": hash})
}

func (a *simulationApp) fault() string { a.mu.Lock(); defer a.mu.Unlock(); return a.merchantFault }
func (a *simulationApp) execute(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	a.count("merchant.execute")
	if a.fault() == "execute_error" {
		return nil, errors.New("controlled execution failure")
	}
	return merchant.TextDigest(ctx, request)
}
func (a *simulationApp) observeMerchant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/quote"):
			a.count("merchant.quote")
		case strings.HasSuffix(r.URL.Path, "/accept"):
			a.count("merchant.accept")
		case strings.HasSuffix(r.URL.Path, "/fulfill"):
			a.count("merchant.fulfill")
		case strings.Contains(r.URL.Path, "/results/"):
			a.count("merchant.lookup")
		}
		next.ServeHTTP(w, r)
	})
}

type controlledMerchantStore struct {
	merchant.Store
	app *simulationApp
}

func (s *controlledMerchantStore) Get(ctx context.Context, id string) (merchant.Record, error) {
	if s.app.fault() == "lookup_unavailable" {
		return merchant.Record{}, merchant.ErrUnavailable
	}
	return s.Store.Get(ctx, id)
}
func (s *controlledMerchantStore) Claim(ctx context.Context, id, intent string, event merchant.PaymentEvent) (merchant.Record, bool, error) {
	r, claimed, err := s.Store.Claim(ctx, id, intent, event)
	if err == nil && claimed && s.app.fault() == "claim_lost" {
		return r, false, errors.New("controlled claim acknowledgement loss")
	}
	return r, claimed, err
}
func (s *controlledMerchantStore) Finish(ctx context.Context, id, intent string, outcome merchant.Outcome) (merchant.Record, error) {
	if s.app.fault() == "finish_before" {
		return merchant.Record{}, errors.New("controlled result persistence failure")
	}
	r, err := s.Store.Finish(ctx, id, intent, outcome)
	if err == nil && s.app.fault() == "finish_lost" {
		return r, errors.New("controlled finish acknowledgement loss")
	}
	return r, err
}
