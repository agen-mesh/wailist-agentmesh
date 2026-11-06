package arc

import (
	"math/big"
	"testing"
)

func TestParseUSDCMicros(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1", "1000000"},
		{"1.5", "1500000"},
		{"0.000001", "1"},
		{"1.", "1000000"},
		{".5", "500000"},
		{"1000", "1000000000"},
	}
	for _, tc := range cases {
		got, err := ParseUSDCMicros(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Fatalf("%q: got %s want %s", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "1.2345678", "-1", "+1", "1e2", "1,000", "abc", "0.0000001"} {
		if _, err := ParseUSDCMicros(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestFormatUSDCRoundTrip(t *testing.T) {
	n, err := ParseUSDCMicros("1.25")
	if err != nil {
		t.Fatal(err)
	}
	if FormatUSDC(n) != "1.25" {
		t.Fatalf("format: %s", FormatUSDC(n))
	}
	if FormatUSDC(nil) != "0" {
		t.Fatal("nil")
	}
}

func TestNativeWeiIsNotTheERC20Figure(t *testing.T) {
	// 1 USDC of native gas is 10^18 wei and 10^6 ERC-20 units.
	wei := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	got := NativeWeiToUSDCMicros(wei)
	if got.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("1 USDC native -> %s micro, want 1000000", got)
	}
	// A sub-micro remainder must disappear, not round up into spendable USDC.
	dust := new(big.Int).Add(wei, big.NewInt(999_999_999_999))
	if NativeWeiToUSDCMicros(dust).Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatal("sub-micro native dust changed the ERC-20 view")
	}
}

func TestArcTestnetConstants(t *testing.T) {
	c, ok := ChainByID(Testnet, "arc-testnet")
	if !ok {
		t.Fatal("missing arc-testnet")
	}
	if c.ChainID != 5042002 || c.CAIP2() != "eip155:5042002" || c.Domain != 26 {
		t.Fatalf("arc identity: %+v %s", c.ChainID, c.CAIP2())
	}
	if c.USDC != ArcUSDC || c.EURC == "" || c.GasSymbol != "USDC" {
		t.Fatalf("arc assets: %+v", c)
	}
	if c.TokenMessenger != "0x8FE6B999Dc680CcFDD5Bf7EB0974218be2542DAA" {
		t.Fatal("messenger")
	}
	algo, ok := ChainByID(Testnet, "algorand-testnet")
	if !ok || algo.CCTP() || algo.AlgoAsset != 10458941 {
		t.Fatalf("algorand must stay visible and not be a CCTP domain: %+v", algo)
	}
	if _, ok := ChainByID(Testnet, "ethereum"); ok {
		t.Fatal("testnet mode must not serve mainnet chain ids")
	}
}
