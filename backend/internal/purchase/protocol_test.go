package purchase

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func protocolOrder(t *testing.T) Order {
	t.Helper()
	t.Setenv("ARC_NETWORK", "testnet")
	now := time.Unix(1_800_000_000, 0).UTC()
	key, err := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	o := Order{ID: "order-one", AccountID: "account-one", CreatedAt: now, Request: json.RawMessage(`{"text":"sample"}`), Offer: Offer{Key: "entity-one", Owner: "0x2222222222222222222222222222222222222222", Payee: "0x2222222222222222222222222222222222222222", Version: "1", ContentHash: Digest([]byte("payload")), SnapshotHash: Digest([]byte("snapshot")), Endpoint: "https://merchant.example/execute", Method: "POST", Capability: "text-digest", ChainID: ChainID, Asset: Asset, PriceMicros: "100", ValidUntil: now.Add(time.Hour), ExpiresAtBlock: "1001", ObservedBlock: "1000"}}
	o.Quote = Quote{ID: "challenge", Method: "POST", Resource: o.Offer.Endpoint, RequestDigest: Digest(o.Request), ChainID: ChainID, Asset: Asset, Payee: o.Offer.Payee, AmountMicros: "100", Deadline: now.Add(5 * time.Minute), Proof: "merchant-proof"}
	o.Intent, o.IntentHash, err = NewIntent(o, crypto.PubkeyToAddress(key.PublicKey).Hex(), "150", now)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func signOrder(t *testing.T, o Order) string {
	t.Helper()
	key, _ := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	after, _ := Amount(o.Intent.ValidAfter)
	before, _ := Amount(o.Intent.ValidBefore)
	value, _ := Amount(o.Intent.AmountMicros)
	a, err := arc.SignAuthorization(key, "USDC", "2", ChainID, common.HexToAddress(Asset), arc.Authorization{To: common.HexToAddress(o.Intent.Payee), Value: value, ValidAfter: after, ValidBefore: before, Nonce: common.HexToHash(o.IntentHash)})
	if err != nil {
		t.Fatal(err)
	}
	sig := append(append(append([]byte{}, a.R[:]...), a.S[:]...), a.V)
	return "0x" + hex.EncodeToString(sig)
}

func TestIntentBindsEveryFieldAndSeparatesSalt(t *testing.T) {
	o := protocolOrder(t)
	sig := signOrder(t, o)
	if _, err := Authorization(o, sig); err != nil {
		t.Fatal(err)
	}
	if o.Intent.OrderSalt == o.IntentHash {
		t.Fatal("salt must not be the derived nonce")
	}
	fresh := protocolOrder(t)
	if fresh.IntentHash == o.IntentHash || fresh.Intent.OrderSalt == o.Intent.OrderSalt {
		t.Fatal("independent orders reused salt")
	}
	v := reflect.ValueOf(o.Intent)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		t.Run(name, func(t *testing.T) {
			changed := o
			field := reflect.ValueOf(&changed.Intent).Elem().Field(i)
			if field.Kind() == reflect.String {
				field.SetString(field.String() + "x")
			} else {
				field.SetInt(field.Int() + 1)
			}
			if _, err := Authorization(changed, sig); err == nil {
				t.Fatal("changed intent accepted")
			}
		})
	}
}
func TestIntentRejectsChangedChallengeAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Order)
	}{
		{"payee", func(o *Order) { o.Quote.Payee = o.Intent.Payer }},
		{"amount", func(o *Order) { o.Quote.AmountMicros = "101" }},
		{"resource", func(o *Order) { o.Quote.Resource += "/other" }},
		{"method", func(o *Order) { o.Quote.Method = "GET" }},
		{"request", func(o *Order) { o.Request = json.RawMessage(`{"text":"different"}`) }},
		{"chain", func(o *Order) { o.Quote.ChainID++ }},
		{"owner", func(o *Order) { o.Offer.Owner = o.Intent.Payer }},
		{"native expiry", func(o *Order) { o.Offer.ExpiresAtBlock = o.Offer.ObservedBlock }},
		{"UTC expiry", func(o *Order) { o.Offer.ValidUntil = o.CreatedAt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := protocolOrder(t)
			tc.change(&o)
			if _, _, err := NewIntent(o, o.Intent.Payer, "150", o.CreatedAt); err == nil {
				t.Fatal("changed terms accepted")
			}
		})
	}
}
func TestAmountsNeverAcceptFloatsOrOverflow(t *testing.T) {
	for _, s := range []string{"", "01", "1.0", "-1", "1e3", "9223372036854775808", " 1"} {
		if _, err := Amount(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestWalletProofBindsMessageAndExpiry(t *testing.T) {
	key, _ := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	now := time.Now()
	c := WalletChallenge{Address: crypto.PubkeyToAddress(key.PublicKey).Hex(), Message: "domain account nonce expiry", ExpiresAt: now.Add(time.Minute)}
	sig, err := crypto.Sign(accounts.TextHash([]byte(c.Message)), key)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(sig)
	if err = VerifyWallet(c, encoded, now); err != nil {
		t.Fatal(err)
	}
	changed := c
	changed.Message += "other-account"
	if VerifyWallet(changed, encoded, now) == nil {
		t.Fatal("message substitution accepted")
	}
	if VerifyWallet(c, encoded, now.Add(time.Minute)) == nil {
		t.Fatal("expired link accepted")
	}
}

func TestOrderViewNeverExposesStoredAuthorization(t *testing.T) {
	o := protocolOrder(t)
	o.SignedEnvelope = "secret-envelope"
	out, err := json.Marshal(View(o, o.CreatedAt))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"signedEnvelope", "signature", "accountId", "request"} {
		if _, ok := fields[name]; ok {
			t.Fatalf("exposed %s", name)
		}
	}
}
