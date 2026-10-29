package arc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/testutil"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func policyConfig() Config {
	cfg := testCfg()
	cfg.operatorUserIDs = map[string]struct{}{"owner": {}}
	cfg.PayTo = cfg.Address.Hex()
	return cfg
}

func customerPayment(t *testing.T, cfg Config) string {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	chain, _ := ChainByID(Testnet, "arc-testnet")
	auth, err := SignAuthorization(key, "USDC", "2", chain.ChainID, common.HexToAddress(chain.USDC), Authorization{
		To: common.HexToAddress(cfg.PayTo), Value: big.NewInt(10_000),
		ValidAfter: big.NewInt(0), ValidBefore: big.NewInt(time.Now().Add(time.Hour).Unix()),
		Nonce: crypto.Keccak256Hash([]byte(t.Name())),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"scheme": "exact", "network": chain.CAIP2(),
		"payload": map[string]any{
			"authorization": map[string]string{
				"from": auth.From.Hex(), "to": auth.To.Hex(), "value": auth.Value.String(),
				"validAfter": "0", "validBefore": auth.ValidBefore.String(), "nonce": auth.Nonce.Hex(),
			},
			"signature": auth.signatureHex(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestArcOperatorPolicyCoversEveryServiceWrite(t *testing.T) {
	for _, op := range []string{"transfer", "nanopay", "bridge", "gateway", "complete", "settle"} {
		t.Run(op, func(t *testing.T) {
			cfg := policyConfig()
			rpc := &testutil.ArcRPC{}
			client := &http.Client{Transport: rpc}
			raw := customerPayment(t, cfg)
			execute := func(ctx context.Context, cfg Config, broadcast bool) (Result, error) {
				switch op {
				case "complete":
					return CompleteBridge(ctx, rpc, client, cfg, "ethereum-sepolia", "arc-testnet", common.HexToHash("0x01").Hex())
				case "settle":
					return SettlePayment(ctx, rpc, cfg, raw, big.NewInt(10_000))
				default:
					return Execute(ctx, rpc, cfg, Request{Op: op, ChainID: "arc-testnet", DestChainID: "base-sepolia", To: cfg.Address.Hex(), Amount: "0.01", Broadcast: broadcast})
				}
			}
			for _, ctx := range []context.Context{
				context.Background(), WithPrincipal(context.Background(), "member"),
				context.WithValue(context.Background(), "userID", "owner"),
				context.WithValue(context.Background(), "operator", true),
				context.WithValue(context.Background(), "worldVerified", true),
			} {
				if _, err := execute(ctx, cfg, true); !errors.Is(err, ErrOperatorRequired) {
					t.Fatalf("unauthorized %s: %v", op, err)
				}
			}
			closed := cfg
			closed.operatorUserIDs = nil
			if _, err := execute(WithPrincipal(context.Background(), "owner"), closed, true); !errors.Is(err, ErrOperatorRequired) {
				t.Fatalf("empty policy: %v", err)
			}
			if rpc.Calls.Load() != 0 {
				t.Fatalf("denied operation made %d calls", rpc.Calls.Load())
			}
			if op != "complete" && op != "settle" {
				res, err := execute(context.Background(), closed, false)
				if err != nil || res.Status != "quoted" || rpc.Calls.Load() != 0 {
					t.Fatalf("quote: %+v %v, calls=%d", res, err, rpc.Calls.Load())
				}
			}
			res, err := execute(WithPrincipal(context.Background(), "owner"), cfg, true)
			if err != nil || res.Status != "success" || rpc.Sends.Load() == 0 {
				t.Fatalf("allowed %s: %+v %v, sends=%d", op, res, err, rpc.Sends.Load())
			}
			if rpc.Signer.Load() != cfg.Address.Hex() {
				t.Fatalf("signer=%v", rpc.Signer.Load())
			}
		})
	}
}

func TestArcOperatorConfigAndDetachedPrincipal(t *testing.T) {
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "")
	t.Setenv("ARC_OPERATOR_USER_IDS", " , owner, second, owner, ")
	cfg := LoadConfig()
	for _, id := range []string{"owner", "second"} {
		if err := cfg.requireOperator(WithPrincipal(context.Background(), id)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"", "Owner", "owner-extra", "*"} {
		if err := cfg.requireOperator(WithPrincipal(context.Background(), id)); !errors.Is(err, ErrOperatorRequired) {
			t.Fatalf("accepted %q", id)
		}
	}
	ctx, cancel := context.WithCancel(WithPrincipal(context.Background(), "owner"))
	ctx = context.WithValue(ctx, "payload", "untrusted")
	cancel()
	detached := DetachedPrincipal(ctx)
	if detached.Err() != nil || detached.Value("payload") != nil || cfg.requireOperator(detached) != nil {
		t.Fatal("detached context did not retain only the principal")
	}
	t.Setenv("ARC_OPERATOR_USER_IDS", "")
	if cfg.requireOperator(detached) != nil {
		t.Fatal("loaded policy changed after environment mutation")
	}
	if err := LoadConfig().requireOperator(detached); !errors.Is(err, ErrOperatorRequired) {
		t.Fatalf("fresh empty policy: %v", err)
	}
}

func TestArcLostBroadcastResponseRetainsTransactionIdentity(t *testing.T) {
	for _, op := range []string{"transfer", "bridge", "gateway"} {
		t.Run(op, func(t *testing.T) {
			cfg := policyConfig()
			rpc := &testutil.ArcRPC{LoseResponse: true}
			res, err := Execute(WithPrincipal(context.Background(), "owner"), rpc, cfg, Request{
				Op: op, ChainID: "arc-testnet", DestChainID: "base-sepolia", To: cfg.Address.Hex(), Amount: "1", Broadcast: true,
			})
			if !errors.Is(err, ErrSubmissionUncertain) || rpc.Sends.Load() != 1 {
				t.Fatalf("result=%+v err=%v sends=%d", res, err, rpc.Sends.Load())
			}
			hash := res.TxHash
			if op != "transfer" {
				hash = res.ApproveTx
			}
			if hash == "" || hash != rpc.Hash.Load() {
				t.Fatalf("transaction identity lost: %+v", res)
			}
		})
	}
}
