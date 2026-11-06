package chain_test

import (
	"math/big"
	"testing"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// domainSeparator recomputes EIP-712's domain hash from a profile the way the
// token contract does, so a wrong TokenName/TokenVersion/ChainID/TokenAddress
// cannot pass unnoticed. Kept local to the test on purpose: the signing code
// in internal/wallet imports this package, so this package cannot import it.
func domainSeparator(p chain.Profile) string {
	typeHash := crypto.Keccak256([]byte(
		"EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	buf := make([]byte, 0, 32*5)
	buf = append(buf, typeHash...)
	buf = append(buf, crypto.Keccak256([]byte(p.TokenName))...)
	buf = append(buf, crypto.Keccak256([]byte(p.TokenVersion))...)
	buf = append(buf, common.LeftPadBytes(big.NewInt(p.ChainID).Bytes(), 32)...)
	buf = append(buf, common.LeftPadBytes(common.HexToAddress(p.TokenAddress).Bytes(), 32)...)
	return common.BytesToHash(crypto.Keccak256(buf)).Hex()
}

// The values read off each chain on the dates named in arc.go / whitechain.go.
// If a profile drifts from what the token actually reports, the authorization
// AgentMesh signs is rejected by the facilitator — so this is the test that
// matters most in this package.
func TestProfileDomainSeparatorsMatchChainState(t *testing.T) {
	for _, tc := range []struct {
		profile chain.Profile
		want    string
	}{
		{chain.Arc, "0x361191522483d32a83e70ae7183b4b9629442c13a78bc9921d6f707911c8c6b0"},
		{chain.Whitechain, "0xcdcfacb6b15c8238b4e5fbb47bdb83b7f83f50a531d264bf9763d2bd1d3d2a25"},
	} {
		if got := domainSeparator(tc.profile); got != tc.want {
			t.Errorf("%s domain separator = %s, want %s", tc.profile.ID, got, tc.want)
		}
	}
}

func TestArcProfileValues(t *testing.T) {
	p := chain.Arc
	if p.ChainID != 5042002 || p.Network != "eip155:5042002" {
		t.Errorf("arc chain id/network = %d/%s", p.ChainID, p.Network)
	}
	if p.TokenAddress != "0x3600000000000000000000000000000000000000" {
		t.Errorf("arc token = %s", p.TokenAddress)
	}
	if p.TokenName != "USDC" || p.TokenVersion != "2" || p.TokenDecimals != 6 {
		t.Errorf("arc EIP-712 token domain = %s/%s/%d", p.TokenName, p.TokenVersion, p.TokenDecimals)
	}
	if p.FacilitatorURL != "https://arc.007575.xyz" {
		t.Errorf("arc facilitator = %s", p.FacilitatorURL)
	}
	// USDC is the gas token on Arc. One asset is the whole argument for the
	// rail, so it is worth a test rather than a comment.
	if p.NativeSymbol != p.TokenSymbol {
		t.Errorf("arc native %s != token %s", p.NativeSymbol, p.TokenSymbol)
	}
}

func TestUseSelectsProfile(t *testing.T) {
	t.Cleanup(func() { chain.Use("") })

	for _, id := range []string{"arc", "ARC", " arc "} {
		chain.Use(id)
		if chain.Active.ID != "arc" || chain.Network != "eip155:5042002" ||
			chain.TokenAddress != chain.Arc.TokenAddress || chain.USDTAddress != chain.Arc.TokenAddress {
			t.Fatalf("Use(%q) left active=%s network=%s token=%s", id, chain.Active.ID, chain.Network, chain.TokenAddress)
		}
		if !chain.IsToken(chain.Arc.TokenAddress) || !chain.IsUSDT(chain.Arc.TokenAddress) {
			t.Fatalf("Use(%q): arc token not recognised as the active asset", id)
		}
		if got := chain.TxURL("0xabc"); got != "https://explorer.testnet.arc.io/tx/0xabc" {
			t.Fatalf("Use(%q): tx url = %s", id, got)
		}
	}

	chain.Use("")
	if chain.Active.ID != "whitechain" || chain.ChainID != 1874 {
		t.Fatalf("empty X402_CHAIN left active=%s chainID=%d", chain.Active.ID, chain.ChainID)
	}
}

// A typo in X402_CHAIN must not silently settle on the other chain's token.
func TestUseRejectsUnknownChain(t *testing.T) {
	t.Cleanup(func() { chain.Use("") })
	defer func() {
		if recover() == nil {
			t.Fatal("Use(\"ethereum\") did not panic")
		}
	}()
	chain.Use("ethereum")
}

// Profiles are pinned data, not views of whatever is active: the Whitechain
// read connectors depend on chain.Whitechain staying Whitechain while Arc
// settles.
func TestProfilesStayPinnedWhenAnotherIsActive(t *testing.T) {
	t.Cleanup(func() { chain.Use("") })
	chain.Use("arc")
	if chain.Whitechain.ChainID != 1874 || chain.Whitechain.TokenSymbol != "USDT" {
		t.Fatalf("whitechain profile mutated: %+v", chain.Whitechain)
	}
	if got := chain.Whitechain.TxURL("0xdef"); got != "https://explorer.testnet.whitechain.io/tx/0xdef" {
		t.Fatalf("whitechain tx url = %s", got)
	}
	if chain.Whitechain.IsToken(chain.Arc.TokenAddress) {
		t.Fatal("arc USDC recognised as whitechain's token")
	}
}

func TestFormatToken(t *testing.T) {
	for in, want := range map[int64]string{0: "0.000000", 10000: "0.010000", 1_500_000: "1.500000", -10000: "-0.010000"} {
		if got := chain.FormatToken(in); got != want {
			t.Errorf("FormatToken(%d) = %s, want %s", in, got, want)
		}
	}
}
