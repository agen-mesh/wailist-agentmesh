# AgentRunEscrow

Reviewed and deployed through Arc Studio. 94 Foundry tests, 7 suites.

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
