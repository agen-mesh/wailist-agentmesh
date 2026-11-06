package purchase

import (
	"fmt"
	"strings"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/common"
)

// RefundPolicy is shown before a customer approves or signs. A confirmed
// payment is a direct transfer to the payee. A failed merchant tool does
// not return it.
const RefundPolicy = "A confirmed USDC payment is sent directly to the payee. If the service fails after that payment, it is not refunded."

// EnabledChain is the Arc settlement chain selected by ARC_NETWORK.
// Purchases stay on testnet until mainnet purchases are configured.
// Orders store the chain they were created for, and later steps reject a
// chain that does not match this record.
func EnabledChain() (arc.Chain, error) {
	cfg := arc.LoadConfig()
	if cfg.Network != arc.Testnet {
		return arc.Chain{}, fmt.Errorf("arc mainnet purchases are not configured")
	}
	chain, ok := arc.ChainByID(cfg.Network, "arc-testnet")
	if !ok || chain.ChainID == 0 || !common.IsHexAddress(chain.USDC) || chain.RPC == "" || chain.ExplorerOrigin() == "" {
		return arc.Chain{}, fmt.Errorf("arc testnet is not configured")
	}
	return chain, nil
}

func matchesEnabledChain(chainID int64, asset string) error {
	chain, err := EnabledChain()
	if err != nil {
		return err
	}
	if chainID != chain.ChainID || !strings.EqualFold(asset, chain.USDC) {
		return fmt.Errorf("order chain does not match the configured Arc network")
	}
	return nil
}
