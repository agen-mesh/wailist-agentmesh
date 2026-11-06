package wallet

import (
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	"github.com/agentmesh/backend/internal/x402"
)

// TypedDataForTest exposes the EIP-712 typed data for golden checks.
func TypedDataForTest(d Domain) apitypes.TypedData { return typedData(d, x402.Authorization{}) }
