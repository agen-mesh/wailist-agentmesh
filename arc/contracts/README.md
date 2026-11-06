# AgentRunEscrow

Written and reviewed through Arc Studio, over two review rounds. 94 Foundry tests across 7 suites
and a Slither run — **Arc Studio's run, not reproduced locally**: the machine this was assembled on
has no Foundry toolchain, so reproduce with the commands below rather than taking the number on
trust.

**Not deployed, and not wired into AgentMesh.** The Arc rail that ships settles x402 payments
against USDC's own EIP-3009 interface and needs no contract (see [`../README.md`](../README.md)).
This escrow is the reviewed next step: an onchain spend ceiling per run, one `CallSettled` receipt
per paid call, and atomic fee splitting — none of which a bare per-call transfer gives.

## Run the tests

```bash
forge install foundry-rs/forge-std OpenZeppelin/openzeppelin-contracts
forge test
```

## Layout

| Path | What |
|---|---|
| `AgentRunEscrow.sol` | The contract. One escrow position per workflow run, many releases against it. |
| `test/AgentRunEscrow.t.sol` | Core lifecycle: open, settle, close, budget boundaries, authorization. |
| `test/AgentRunEscrow.Admin.t.sol` | Owner setters, two-step ownership, disabled renounce. |
| `test/AgentRunEscrow.Batch.t.sol` | `settleCallBatch`, including partial-overspend rollback and the 50-item cap. |
| `test/AgentRunEscrow.Permit2.t.sol` | Witness-bound Permit2 funding against both a faithful and a lax Permit2 mock. |
| `test/AgentRunEscrow.Tokens.t.sol` | Adversarial tokens: silent-false transfers, fee-on-transfer, reentrancy hooks. |
| `test/AgentRunEscrow.Fuzz.t.sol` | Fuzzed amounts and fee rates. |
| `test/AgentRunEscrow.Invariant.t.sol` | Invariant: `spent` never exceeds `budget`. |
| `test/mocks/` | `MockUSDC`, `FalseReturnUSDC`, `FeeOnTransferUSDC`, `HookUSDC`, `ReentrantActor`, Permit2 mocks. |
