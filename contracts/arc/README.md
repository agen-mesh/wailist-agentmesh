# AgentMesh escrow on Arc

`AgentMeshEscrow` holds ERC-20 USDC for a deal. `fund` pulls tokens with `transferFrom` after the deal is marked funded. `release` pays the payee. `refund` returns the tokens to the payer. Amounts are 6-decimal base units. The contract does not read the native balance.

## Deploy from Arc Studio

1. Open https://studio.arc.com and create a project.
2. Paste `STUDIO_PROMPT.md` into the agent.
3. Deploy to Arc testnet. Studio does not deploy mainnet.
4. Copy the project URL into `ARC_STUDIO_URL` and the contract address into `ARC_ESCROW_ADDRESS` on the AgentMesh server.

## Deploy with Foundry

```bash
cd contracts/arc
forge create src/AgentMeshEscrow.sol:AgentMeshEscrow \
  --rpc-url arc_testnet \
  --private-key "$ARC_SPEND_PRIVATE_KEY" \
  --broadcast
```

Fund a deal only after the payer has approved this contract for the USDC amount. On Arc testnet that token is `0x3600000000000000000000000000000000000000`.

Gas on Arc is USDC. Get testnet USDC from https://faucet.circle.com.
