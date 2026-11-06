# Arc Studio prompt

Paste this into a new project at https://studio.arc.com. Deploy to Arc testnet. Put the resulting project URL in the AgentMesh server env as `ARC_STUDIO_URL`, and the deployed address as `ARC_ESCROW_ADDRESS`.

---

Build and deploy `AgentMeshEscrow` on Arc testnet.

The contract escrows ERC-20 USDC, 6 decimals, at `0x3600000000000000000000000000000000000000`. Do not read `address(this).balance`. On Arc that native balance is the same USDC at 18 decimals, and adding it to the ERC-20 amount double-counts or mis-scales the funds.

Functions:

- `fund(bytes32 id, address token, address payee, uint256 amount)` — reject a zero token, zero payee, zero amount, and an id that is already funded. Write status `1` and the payer, payee, token, and amount before `transferFrom` the payer.
- `release(bytes32 id)` — only the payer or payee, and only while status is `1`. Write status `2`, then transfer the stored amount to the payee.
- `refund(bytes32 id)` — same callers. Write status `3`, then transfer the stored amount back to the payer.

Use checks-effects-interactions. Solidity 0.8.24. No admin key, no upgrade proxy, no native-value functions.

After deploy, show the contract address, the Arc testnet explorer link, and a short note that AgentMesh will call this address only when `ARC_ESCROW_ADDRESS` is set. Do not print or store a private key in the repository.
