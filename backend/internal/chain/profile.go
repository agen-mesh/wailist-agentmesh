// Package chain holds the single source of truth for the blockchain AgentMesh
// settles x402 payments on. There are two rails, and exactly one is active per
// process:
//
//	X402_CHAIN unset | "whitechain"  ->  Whitechain Testnet, USDT  (the default)
//	X402_CHAIN=arc                   ->  Arc Testnet, USDC
//
// Both settle through a hosted x402 facilitator over the v2 "exact" scheme:
// the payer signs an EIP-3009 TransferWithAuthorization against the token's
// own EIP-712 domain and the facilitator's relayer submits it and pays the
// gas. Nothing in this package is chain-agnostic by accident — every value a
// signature or a settlement depends on lives in a Profile, so adding a rail is
// a profile, not a code path.
//
// Call sites read the package-level vars (chain.Network, chain.TokenAddress,
// chain.TxURL, …), which mirror the ACTIVE profile. Code that is pinned to one
// chain regardless of which rail is settling — the Whitechain read connectors,
// for instance — must read chain.Whitechain.X explicitly instead.
package chain

import (
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Profile is everything that differs between the chains AgentMesh can settle
// on. Every field is load-bearing for either a signature (ChainID, TokenName,
// TokenVersion, TokenAddress) or a settlement (Network, FacilitatorURL,
// MinSettleMicros), so none of it may be guessed at runtime.
type Profile struct {
	// ID is the X402_CHAIN value that selects this profile.
	ID string
	// Name is the human-readable chain name for logs and UI copy.
	Name string
	// Network is the CAIP-2 id carried in every x402 challenge and payload.
	Network string
	// ChainID is the EVM chain id, and the EIP-712 domain's chainId.
	ChainID int64

	RPCURL         string
	ExplorerURL    string
	FacilitatorURL string
	// FaucetURL is where a human tops up the asset a payer actually needs.
	FaucetURL string

	// TokenAddress is the EIP-3009-capable ERC-20 the facilitator allowlists.
	// It is the EIP-712 verifyingContract.
	TokenAddress string
	// TokenName and TokenVersion are that token's EIP-712 domain fields. A
	// wrong value here produces a signature the facilitator rejects, so each
	// profile documents how it was verified.
	TokenName    string
	TokenVersion string
	TokenSymbol  string
	// TokenDecimals is 6 on both rails, which is what keeps the codebase's
	// existing "USD micros" arithmetic unchanged: amounts are base units, so
	// 10000 == 0.01 of the token.
	TokenDecimals int

	// NativeSymbol/NativeDecimals describe the gas token. Payers never need
	// it — the facilitator's relayer submits the transfer and pays the gas —
	// and on Arc it is the same asset being paid anyway. Display only;
	// nothing in this rail prices against the native representation.
	NativeSymbol   string
	NativeDecimals int

	// MinSettleMicros is the facilitator's minimum payment in base units.
	// Anything smaller comes back amount_below_minimum.
	MinSettleMicros int64
}

// Active is the profile this process settles on, chosen by X402_CHAIN.
var Active = Whitechain

// Package-level mirrors of Active, so the ~120 existing call sites keep
// reading chain.Network / chain.TokenAddress / … unchanged.
var (
	ChainID int64
	Network string
	Name    string

	DefaultRPCURL         string
	DefaultExplorerURL    string
	DefaultFacilitatorURL string
	DefaultFaucetURL      string

	TokenAddress  string
	TokenName     string
	TokenVersion  string
	TokenSymbol   string
	TokenDecimals int

	NativeSymbol   string
	NativeDecimals int

	MinSettleMicros int64

	// USDTAddress is the pre-Arc name for TokenAddress, kept so existing
	// call sites do not churn. Under the arc profile it holds USDC.
	USDTAddress string
)

func init() { Use(os.Getenv("X402_CHAIN")) }

// Use selects a profile by its X402_CHAIN id. An empty id is the default
// rail. An unrecognised id panics rather than falling back: this decides
// which token real money is signed away in, and a typo must not settle
// anything.
func Use(id string) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", Whitechain.ID:
		apply(Whitechain)
	case Arc.ID:
		apply(Arc)
	default:
		panic("chain: unknown X402_CHAIN " + id + " (want " + Whitechain.ID + " or " + Arc.ID + ")")
	}
}

func apply(p Profile) {
	Active = p
	ChainID, Network, Name = p.ChainID, p.Network, p.Name
	DefaultRPCURL, DefaultExplorerURL = p.RPCURL, p.ExplorerURL
	DefaultFacilitatorURL, DefaultFaucetURL = p.FacilitatorURL, p.FaucetURL
	TokenAddress, USDTAddress = p.TokenAddress, p.TokenAddress
	TokenName, TokenVersion, TokenSymbol = p.TokenName, p.TokenVersion, p.TokenSymbol
	TokenDecimals = p.TokenDecimals
	NativeSymbol, NativeDecimals = p.NativeSymbol, p.NativeDecimals
	MinSettleMicros = p.MinSettleMicros
}

// Extra is the `extra` object every x402 accepts entry on this chain carries:
// the EIP-712 domain name/version a client needs to sign the authorization.
func (p Profile) Extra() map[string]any {
	return map[string]any{"name": p.TokenName, "version": p.TokenVersion}
}

// TxURL links a transaction hash to this chain's explorer. Empty in, empty out.
func (p Profile) TxURL(hash string) string {
	if hash == "" {
		return ""
	}
	return p.ExplorerURL + "/tx/" + hash
}

// AddressURL links an account to this chain's explorer.
func (p Profile) AddressURL(addr string) string {
	if addr == "" {
		return ""
	}
	return p.ExplorerURL + "/address/" + addr
}

// IsToken reports whether asset names this profile's allowlisted token.
func (p Profile) IsToken(asset string) bool { return SameAddress(asset, p.TokenAddress) }

// FormatToken renders base units as a 6-decimal string, e.g. "0.010000".
func (p Profile) FormatToken(micros int64) string {
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

// The package-level helpers below operate on the ACTIVE profile.

func Extra() map[string]any            { return Active.Extra() }
func TxURL(hash string) string         { return Active.TxURL(hash) }
func AddressURL(addr string) string    { return Active.AddressURL(addr) }
func IsToken(asset string) bool        { return Active.IsToken(asset) }
func FormatToken(micros int64) string  { return Active.FormatToken(micros) }

// IsUSDT and FormatUSDT are the pre-Arc names for IsToken and FormatToken,
// kept so existing call sites do not churn.
func IsUSDT(asset string) bool        { return Active.IsToken(asset) }
func FormatUSDT(micros int64) string  { return Active.FormatToken(micros) }

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
