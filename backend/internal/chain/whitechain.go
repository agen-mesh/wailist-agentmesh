// Package chain holds the single source of truth for the blockchain
// AgentMesh settles x402 payments on: Whitechain testnet (chain 1874),
// paying in USDT through the hosted Whitechain x402 facilitator.
//
// Every value here was checked live on 2026-11-06: the token's on-chain
// DOMAIN_SEPARATOR equals the one recomputed from TokenName/TokenVersion/
// ChainID/USDTAddress, and the facilitator's /supported + /assets report the
// same network and asset.
package chain

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	// ChainID is Whitechain testnet's EVM chain id.
	ChainID = 1874
	// Network is the CAIP-2 id carried in every x402 challenge and payload.
	Network = "eip155:1874"
	// Name is the human-readable chain name for logs and UI copy.
	Name = "Whitechain Testnet"

	DefaultRPCURL         = "https://rpc.testnet.whitechain.io"
	DefaultExplorerURL    = "https://explorer.testnet.whitechain.io"
	DefaultFacilitatorURL = "https://whitechain.007575.xyz"

	// USDTAddress is the EIP-3009-capable USDT token the facilitator
	// allowlists on chain 1874. It is the EIP-712 verifyingContract.
	USDTAddress = "0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7"
	// TokenName / TokenVersion are the token's EIP-712 domain. The token has
	// no version() getter (the call reverts), so the version is pinned here
	// from the facilitator's /assets, verified against DOMAIN_SEPARATOR().
	TokenName    = "Tether USD"
	TokenVersion = "1"
	TokenSymbol  = "USDT"
	// TokenDecimals: amounts are base units, so 10000 == 0.01 USDT, which
	// keeps the codebase's existing "USD micros" arithmetic unchanged.
	TokenDecimals = 6

	// NativeSymbol is the gas token. Payers never need it: the facilitator's
	// relayer submits transferWithAuthorization and pays the gas.
	NativeSymbol   = "WBT"
	NativeDecimals = 18

	// MinSettleMicros is the facilitator's minimum payment (0.01 USDT).
	// Anything smaller is refused with amount_below_minimum.
	MinSettleMicros int64 = 10000
)

// Extra is the `extra` object every x402 accepts entry on this chain carries:
// the EIP-712 domain name/version a client needs to sign the authorization.
func Extra() map[string]any {
	return map[string]any{"name": TokenName, "version": TokenVersion}
}

// TxURL links a transaction hash to the testnet explorer. Empty in, empty out.
func TxURL(hash string) string {
	if hash == "" {
		return ""
	}
	return DefaultExplorerURL + "/tx/" + hash
}

// AddressURL links an account to the testnet explorer.
func AddressURL(addr string) string {
	if addr == "" {
		return ""
	}
	return DefaultExplorerURL + "/address/" + addr
}

// IsAddress reports whether s is a 0x-prefixed 20-byte hex address.
func IsAddress(s string) bool {
	return strings.HasPrefix(s, "0x") && common.IsHexAddress(s)
}

// SameAddress compares two EVM addresses case-insensitively (EIP-55
// checksumming only changes letter case). Invalid input never matches.
func SameAddress(a, b string) bool {
	if !IsAddress(a) || !IsAddress(b) {
		return false
	}
	return common.HexToAddress(a) == common.HexToAddress(b)
}

// IsUSDT reports whether asset names the allowlisted USDT token.
func IsUSDT(asset string) bool { return SameAddress(asset, USDTAddress) }

// FormatUSDT renders base units as a 6-decimal USDT string, e.g. "0.010000".
func FormatUSDT(micros int64) string {
	neg := micros < 0
	if neg {
		micros = -micros
	}
	whole, frac := micros/1_000_000, micros%1_000_000
	s := itoa(whole) + "." + pad6(frac)
	if neg {
		return "-" + s
	}
	return s
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func pad6(n int64) string {
	s := itoa(n)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}
