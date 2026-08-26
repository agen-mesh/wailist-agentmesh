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
// Updated 2026-08-19 reconciling PR #65 ("tiered node expansion") against
// master: billing.go/runfund.go/tendril.go/x402relay.go moved on master
// while this branch was open, for reasons unrelated to anything in the PR
// itself --
//   - billing.go: BillableFlatFee gained models.NodeTypeGoogle as a billable
//     node type, for master's new Google connector nodes.
//   - runfund.go / x402relay.go: both now build their Bazaar discovery
//     extension through the shared nodes.BazaarDiscoveryExtension instead of
//     two hand-maintained, independently-drifting copies (the drift itself
//     was a real prior cataloging bug on master, already fixed there).
//   - tendril.go: emptyRunContext gained LastOutput() any, for the
//     RunContexter interface's own determinism fix (see engine/context.go).
// Every one of these is a clean merge from master, not a local edit; the
// payment amounts/addresses/signing logic (the thing this test actually
// guards) is unchanged. Digests below reflect the merged state.
//
// Updated again 2026-08-26, rebasing PR #65 onto current master, which moved
// further while this PR was still open --
//   - billing.go: BillableFlatFee gained "websearch" alongside "http" as a
//     billable Tool template, for master's new Gemini-grounded web search
//     tool (a real paid call on the platform's own key).
//   - runfund.go / x402relay.go: master added SettleRunTotal (mirrors the
//     existing SettlePlatformFee/FundRunReserve self-settle pattern for a
//     new "whole run's non-tool402 billable total" lump-sum settlement) and
//     its accompanying X402RunTotalInfo resource-info handler.
// Both are additive: new billable-template case, new settlement function,
// new info handler. No existing amount/address/signing logic changed.
// Digests below reflect the merged state.
var frozenX402Files = map[string]string{
	"nodes/tool402.go":             "cf7b39ecf298b2cc427d3c4d40430bc2b7bcb460af73efb1246fd6583bc40055",
	"nodes/runfund.go":             "cd92b318daaced1a0724f8051eeb399954a833c882b0b871bd76d460d79402b3",
	"nodes/walletpay.go":           "98bb3f7d0cb167f8a50d050e04720738c63c68b9fd570758fa5b9604338a4e37",
	"nodes/tendril.go":             "b787a18f17bc80f593159e46a0c7fd7e543a9db44f55a451ed8f47102fb9132a",
	"nodes/billing.go":             "08ee13b175aa43bea258ca263180054057aa4e29c2d9a170059dac0071836cb6",
	"nodes/tier.go":                "5718a3538e042c9d7f90b37f38b47d893644d6093f560d103ea9036c90ddc90b",
	"../api/handlers/x402relay.go": "eacd56896816a213dd5658aa536c704db22362a5d787113cbf269d7fe7c1d858",
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
