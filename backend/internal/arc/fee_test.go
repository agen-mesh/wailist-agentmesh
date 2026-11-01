package arc

import (
	"math/big"
	"testing"
)

func TestFeeSuggestionFloorsArcBaseFee(t *testing.T) {
	floor := big.NewInt(20_000_000_000)
	tip, fee := FeeSuggestion(5042002, nil, big.NewInt(1))
	if tip.Cmp(floor) < 0 || fee.Cmp(floor) < 0 || fee.Cmp(tip) < 0 {
		t.Fatalf("testnet tip=%s fee=%s", tip, fee)
	}
	tip, fee = FeeSuggestion(5042, big.NewInt(1), big.NewInt(1))
	if tip.Cmp(floor) < 0 || fee.Cmp(floor) < 0 {
		t.Fatalf("mainnet tip=%s fee=%s", tip, fee)
	}
	tip, fee = FeeSuggestion(1, big.NewInt(2), big.NewInt(3))
	if tip.Cmp(big.NewInt(2)) != 0 || fee.Cmp(big.NewInt(6)) != 0 {
		t.Fatalf("other chain tip=%s fee=%s", tip, fee)
	}
	high := big.NewInt(50_000_000_000)
	tip, fee = FeeSuggestion(5042002, high, big.NewInt(1))
	if tip.Cmp(high) != 0 || fee.Cmp(high) < 0 {
		t.Fatalf("priority above floor tip=%s fee=%s", tip, fee)
	}
}
