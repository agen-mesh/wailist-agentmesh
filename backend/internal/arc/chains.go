// Package arc is AgentMesh's Arc and multichain USDC treasury.
//
// Amounts that move are ERC-20 base units (6 decimals). On Arc the native
// gas balance is the same USDC with 18 decimals; callers must not add the
// two figures. See docs/arc/ARCHITECTURE.md.
package arc

import "strings"

const (
	// USDC ERC-20 interface on Arc. Native gas is this same asset.
	ArcUSDC = "0x3600000000000000000000000000000000000000"

	// Blocklisted is the public Arc testnet address that reverts on value
	// transfer. Rejecting it before signing saves the sender the gas.
	Blocklisted = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"

	testnetMessenger   = "0x8FE6B999Dc680CcFDD5Bf7EB0974218be2542DAA"
	testnetTransmitter = "0xE737e5cEBEEBa77EFE34D4aa090756590b1CE275"
	mainnetMessenger   = "0x28b5a0e9C621a5BadaA536219b3a228C8168cf5d"
	mainnetTransmitter = "0x81D40F21F12A8F0E3252Bccb954D722d4c464B64"

	testnetGatewayWallet = "0x0077777d7EBA4688BDeF3E311b846F25870A19B9"
	testnetGatewayMinter = "0x0022222ABE238Cc2C7Bb1f21003F0a260052475B"
	mainnetGatewayWallet = "0x77777777Dcc4d5A8B6E418Fd04D8997ef11000eE"
	mainnetGatewayMinter = "0x2222222d7164433c4C09B0b0D809a9b52C04C205"

	arcEURCTestnet = "0x89B50855Aa3bE2F677cD6303Cec089B5F319D72a"
	arcEURCMainnet = "0xbEf5f6d51CB62b58e6A8f77868681825C6fe21c1"

	// Standard CCTP finality. Fast transfer is not available when Arc is the source.
	FinalityStandard uint32 = 2000
)

// Network is the server's settlement mode. Testnet is the default.
type Network string

const (
	Testnet Network = "testnet"
	Mainnet Network = "mainnet"
)

// Chain is one network the treasury knows how to read, and maybe pay on.
type Chain struct {
	ID                 string
	Name               string
	Family             string // "evm" or "algorand"
	ChainID            int64
	Domain             int // CCTP domain, or -1 when this chain is not a CCTP domain
	RPC                string
	Explorer           string
	USDC               string
	EURC               string
	GasSymbol          string
	TokenMessenger     string
	MessageTransmitter string
	GatewayWallet      string
	GatewayMinter      string
	AlgoAsset          uint64
	AlgoIndexer        string
	Testnet            bool
}

// CAIP2 is the network id used in x402 payment requirements.
func (c Chain) CAIP2() string {
	if c.Family == "algorand" {
		if c.Testnet {
			return "algorand:testnet"
		}
		return "algorand:mainnet"
	}
	return "eip155:" + itoa(c.ChainID)
}

// CCTP reports whether USDC can be burned here for minting elsewhere.
func (c Chain) CCTP() bool {
	return c.Family == "evm" && c.Domain >= 0 && c.TokenMessenger != "" && c.USDC != ""
}

// Gateway reports whether a Gateway wallet contract is deployed here.
func (c Chain) Gateway() bool {
	return c.Family == "evm" && c.GatewayWallet != ""
}

// TxURL is the explorer page for a transaction hash or Algorand tx id.
func (c Chain) TxURL(hash string) string {
	if hash == "" || c.Explorer == "" {
		return ""
	}
	return c.Explorer + hash
}

// ExplorerOrigin is the explorer site, without the transaction path.
// Wallet add-chain and purchase receipts use the same registry field as TxURL.
func (c Chain) ExplorerOrigin() string {
	origin := strings.TrimSuffix(c.Explorer, "/")
	origin = strings.TrimSuffix(origin, "/tx")
	return origin
}

// Chains returns the chains for one mode. Testnet and mainnet are separate
// lists so a saved workflow cannot silently change networks.
func Chains(n Network) []Chain {
	if n == Mainnet {
		return mainnetChains()
	}
	return testnetChains()
}

// ChainByID finds a chain in the mode the server is running.
func ChainByID(n Network, id string) (Chain, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, c := range Chains(n) {
		if c.ID == id {
			return c, true
		}
	}
	return Chain{}, false
}

// ParseNetwork accepts "mainnet" and treats everything else as testnet,
// except the empty string, which is also testnet. "mainnet" is the only
// opt-in.
func ParseNetwork(s string) Network {
	if strings.EqualFold(strings.TrimSpace(s), "mainnet") {
		return Mainnet
	}
	return Testnet
}

func IrisHost(testnet bool) string {
	if testnet {
		return "https://iris-api-sandbox.circle.com"
	}
	return "https://iris-api.circle.com"
}

func GatewayHost(testnet bool) string {
	if testnet {
		return "https://gateway-api-testnet.circle.com"
	}
	return "https://gateway-api.circle.com"
}

func testnetChains() []Chain {
	evm := func(id, name string, chainID int64, domain int, rpc, explorer, usdc, gas string) Chain {
		return Chain{
			ID: id, Name: name, Family: "evm", ChainID: chainID, Domain: domain,
			RPC: rpc, Explorer: explorer, USDC: usdc, GasSymbol: gas, Testnet: true,
			TokenMessenger: testnetMessenger, MessageTransmitter: testnetTransmitter,
			GatewayWallet: testnetGatewayWallet, GatewayMinter: testnetGatewayMinter,
		}
	}
	arc := evm("arc-testnet", "Arc Testnet", 5042002, 26,
		"https://rpc.testnet.arc.io", "https://explorer.testnet.arc.io/tx/",
		ArcUSDC, "USDC")
	arc.EURC = arcEURCTestnet
	return []Chain{
		arc,
		evm("ethereum-sepolia", "Ethereum Sepolia", 11155111, 0,
			"https://ethereum-sepolia-rpc.publicnode.com", "https://sepolia.etherscan.io/tx/",
			"0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238", "ETH"),
		evm("base-sepolia", "Base Sepolia", 84532, 6,
			"https://sepolia.base.org", "https://sepolia.basescan.org/tx/",
			"0x036CbD53842c5426634e7929541eC2318f3dCF7e", "ETH"),
		evm("arbitrum-sepolia", "Arbitrum Sepolia", 421614, 3,
			"https://sepolia-rollup.arbitrum.io/rpc", "https://sepolia.arbiscan.io/tx/",
			"0x75faf114eafb1BDbe2F0316DF893fd58CE46AA4d", "ETH"),
		evm("op-sepolia", "OP Sepolia", 11155420, 2,
			"https://sepolia.optimism.io", "https://sepolia-optimism.etherscan.io/tx/",
			"0x5fd84259d66Cd46123540766Be93DFE6D43130D7", "ETH"),
		evm("polygon-amoy", "Polygon Amoy", 80002, 7,
			"https://polygon-amoy-bor-rpc.publicnode.com", "https://amoy.polygonscan.com/tx/",
			"0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582", "POL"),
		evm("avalanche-fuji", "Avalanche Fuji", 43113, 1,
			"https://api.avax-test.network/ext/bc/C/rpc", "https://testnet.snowtrace.io/tx/",
			"0x5425890298aed601595a70AB815c96711a31Bc65", "AVAX"),
		{
			ID: "algorand-testnet", Name: "Algorand Testnet", Family: "algorand",
			Domain: -1, Explorer: "https://lora.algokit.io/testnet/transaction/",
			GasSymbol: "ALGO", AlgoAsset: 10458941,
			AlgoIndexer: "https://testnet-idx.algonode.cloud", Testnet: true,
		},
	}
}

func mainnetChains() []Chain {
	evm := func(id, name string, chainID int64, domain int, rpc, explorer, usdc, gas string) Chain {
		return Chain{
			ID: id, Name: name, Family: "evm", ChainID: chainID, Domain: domain,
			RPC: rpc, Explorer: explorer, USDC: usdc, GasSymbol: gas, Testnet: false,
			TokenMessenger: mainnetMessenger, MessageTransmitter: mainnetTransmitter,
			GatewayWallet: mainnetGatewayWallet, GatewayMinter: mainnetGatewayMinter,
		}
	}
	arc := evm("arc", "Arc", 5042, 26,
		"https://rpc.mainnet.arc.io", "https://explorer.arc.io/tx/",
		ArcUSDC, "USDC")
	arc.EURC = arcEURCMainnet
	return []Chain{
		arc,
		evm("ethereum", "Ethereum", 1, 0,
			"https://ethereum-rpc.publicnode.com", "https://etherscan.io/tx/",
			"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "ETH"),
		evm("base", "Base", 8453, 6,
			"https://mainnet.base.org", "https://basescan.org/tx/",
			"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", "ETH"),
		evm("arbitrum", "Arbitrum", 42161, 3,
			"https://arb1.arbitrum.io/rpc", "https://arbiscan.io/tx/",
			"0xaf88d065e77c8cC2239327C5EDb3A432268e5831", "ETH"),
		evm("optimism", "OP Mainnet", 10, 2,
			"https://mainnet.optimism.io", "https://optimistic.etherscan.io/tx/",
			"0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85", "ETH"),
		evm("polygon", "Polygon", 137, 7,
			"https://polygon-rpc.com", "https://polygonscan.com/tx/",
			"0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", "POL"),
		evm("avalanche", "Avalanche", 43114, 1,
			"https://api.avax.network/ext/bc/C/rpc", "https://snowtrace.io/tx/",
			"0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E", "AVAX"),
		{
			ID: "algorand", Name: "Algorand", Family: "algorand",
			Domain: -1, Explorer: "https://lora.algokit.io/mainnet/transaction/",
			GasSymbol: "ALGO", AlgoAsset: 31566704,
			AlgoIndexer: "https://mainnet-idx.algonode.cloud", Testnet: false,
		},
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
