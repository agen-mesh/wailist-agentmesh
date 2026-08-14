package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// frozenX402Files pins the byte content of the x402 / Tendril payment path.
//
// The wallet topology these files implement is:
//   Wallet 1 (PLATFORM_SPEND_WALLET) --USDC--> Wallet 2 (PLATFORM_WALLET)
//       settled via the GoPlausible facilitator   [inbound leg]
//   Wallet 2 --USDC--> the target endpoint's payTo [outbound leg]
//   Wallet 1 --USDC--> Wallet 2, flat platform markup [markup leg]
//
// Real mainnet USDC moves through this. A change here is never incidental.
//
// If this test fails and you DID intend the change: re-run the hash command in
// the failure message, paste the new digest below, and get explicit sign-off in
// the PR description explaining why the payment path moved. If you did NOT
// intend it, revert the file.
var frozenX402Files = map[string]string{
	"nodes/tool402.go":             "cf7b39ecf298b2cc427d3c4d40430bc2b7bcb460af73efb1246fd6583bc40055",
	"nodes/runfund.go":             "ca3b55214902a37c37f66813901fc6fccf293b52a5597570546d0067473b6cbb",
	"nodes/walletpay.go":           "98bb3f7d0cb167f8a50d050e04720738c63c68b9fd570758fa5b9604338a4e37",
	"nodes/tendril.go":             "6ce9a70c053c6b625f1e87ba4fea5b6d011eedafd89cc474e528a462d3e105fe",
	"nodes/billing.go":             "4e98138e4e067a11385596f27006ead5a4977e4d26f497c9bb08055a0540145c",
	"nodes/tier.go":                "5718a3538e042c9d7f90b37f38b47d893644d6093f560d103ea9036c90ddc90b",
	"../api/handlers/x402relay.go": "4186adfe17affb2a5e008b5ab56c88f264e84f4e7ba9f1ee5fe6090eb21548c1",
	"../x402/facilitator.go":       "976d118ae200994728f96733dceca79bc90fcc2cc99e859c47d710477f9480ca",
}

func TestX402PaymentPathIsFrozen(t *testing.T) {
	for rel, want := range frozenX402Files {
		b, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("frozen file %s is unreadable — was it moved or deleted? %v", rel, err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		if want == "" {
			t.Fatalf("no baseline digest recorded for %s.\n"+
				"Record it with:\n  shasum -a 256 backend/internal/engine/%s\n"+
				"then paste it into frozenX402Files.", rel, rel)
		}
		if got != want {
			t.Errorf("FROZEN FILE CHANGED: %s\n  want %s\n  got  %s\n\n"+
				"This file implements the Wallet 1 -> Wallet 2 -> provider payment path.\n"+
				"If this change is intentional, update the digest AND justify it in the PR.\n"+
				"If not, revert it.", rel, want, got)
		}
	}
}
