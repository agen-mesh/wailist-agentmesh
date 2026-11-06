package chain

// Whitechain is AgentMesh's original EVM settlement rail: Whitechain testnet
// (chain 1874), paying in USDT through the hosted Whitechain x402
// facilitator. It is the default profile.
//
// Every value here was checked live on 2026-11-06: the token's on-chain
// DOMAIN_SEPARATOR equals the one recomputed from TokenName/TokenVersion/
// ChainID/TokenAddress, and the facilitator's /supported + /assets report the
// same network and asset.
var Whitechain = Profile{
	ID:      "whitechain",
	Name:    "Whitechain Testnet",
	Network: "eip155:1874",
	ChainID: 1874,

	RPCURL:         "https://rpc.testnet.whitechain.io",
	ExplorerURL:    "https://explorer.testnet.whitechain.io",
	FacilitatorURL: "https://whitechain.007575.xyz",
	// WBT, the gas token. Payers do not need it (the facilitator relays),
	// but there is no public USDT faucet on this chain at all: only the
	// token owner can mint, so a payer's USDT has to come from them.
	FaucetURL: "https://faucet.testnet.whitechain.io",

	// The EIP-3009-capable USDT the facilitator allowlists on chain 1874.
	TokenAddress: "0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7",
	// The token has no version() getter (the call reverts), so the version
	// is pinned here from the facilitator's /assets, verified against
	// DOMAIN_SEPARATOR().
	TokenName:     "Tether USD",
	TokenVersion:  "1",
	TokenSymbol:   "USDT",
	TokenDecimals: 6,

	NativeSymbol:   "WBT",
	NativeDecimals: 18,

	// The facilitator's minimum payment (0.01 USDT).
	MinSettleMicros: 10000,
}
