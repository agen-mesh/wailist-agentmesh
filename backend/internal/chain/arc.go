package chain

// Arc is the Arc Testnet rail: Circle's stablecoin-native L1, paying in USDC
// through the hosted Arc x402 facilitator. Selected with X402_CHAIN=arc.
//
// The point of this rail, and the reason AgentMesh wants it: on Arc, USDC is
// the native gas token. A payer holds exactly one asset. On a chain where fees
// are paid in a separate volatile token, an autonomous agent that runs that
// token down is stranded holding stablecoins it cannot move; here that failure
// mode does not exist.
//
// Every value below was verified live on 2026-11-07, both against the
// facilitator and independently against chain state:
//
//	https://arc.007575.xyz/supported  -> "exact" on eip155:5042002 (and eip155:5042)
//	https://arc.007575.xyz/assets     -> this asset, 6 decimals, extra {name USDC, version 2},
//	                                     domainSeparator 0x361191522483d32a83e70ae7183b4b9629442c13a78bc9921d6f707911c8c6b0
//	https://rpc.testnet.arc.io        -> eth_chainId 0x4cef52 (5042002); on the token,
//	                                     name() "USDC", version() "2", decimals() 6, and
//	                                     DOMAIN_SEPARATOR() returning that same value.
//
// The separators matching is what makes the EIP-712 domain below safe to sign
// against: it is read back from the token, not taken on the facilitator's word.
var Arc = Profile{
	ID:      "arc",
	Name:    "Arc Testnet",
	Network: "eip155:5042002",
	ChainID: 5042002,

	RPCURL:         "https://rpc.testnet.arc.io",
	ExplorerURL:    "https://explorer.testnet.arc.io",
	FacilitatorURL: "https://arc.007575.xyz",
	FaucetURL:      "https://faucet.circle.com",

	// USDC's 6-decimal ERC-20 interface. Arc exposes the same balance twice:
	// natively at 18 decimals (msg.value, eth_getBalance, gas accounting) and
	// through this ERC-20 at 6, matching USDC everywhere else. This rail uses
	// the 6-decimal interface and only that one — the two scales meeting
	// anywhere would misprice a payment by 10^12.
	TokenAddress:  "0x3600000000000000000000000000000000000000",
	TokenName:     "USDC",
	TokenVersion:  "2",
	TokenSymbol:   "USDC",
	TokenDecimals: 6,

	// Same asset as above, at its native scale. Display only.
	NativeSymbol:   "USDC",
	NativeDecimals: 18,

	// 0.01 USDC — the price of every endpoint in Arc's x402 discovery
	// catalog, and the facilitator's floor.
	MinSettleMicros: 10000,
}
