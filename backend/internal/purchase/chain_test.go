package purchase

import (
	"strings"
	"testing"
)

func TestEnabledChainMatchesPurchaseConstants(t *testing.T) {
	t.Setenv("ARC_NETWORK", "testnet")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "")
	chain, err := EnabledChain()
	if err != nil || chain.ChainID != ChainID || !strings.EqualFold(chain.USDC, Asset) || chain.RPC == "" || chain.ExplorerOrigin() == "" {
		t.Fatalf("enabled=%+v err=%v constants=%d %s", chain, err, ChainID, Asset)
	}
	t.Setenv("ARC_NETWORK", "mainnet")
	if _, err = EnabledChain(); err == nil || !strings.Contains(err.Error(), "mainnet") {
		t.Fatalf("mainnet purchases: %v", err)
	}
}
