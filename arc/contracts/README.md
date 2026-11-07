# AgentRunEscrow

Written and reviewed through Arc Studio, over two review rounds. 94 Foundry tests across 7 suites
and a Slither run — **Arc Studio's run, not reproduced locally**: the machine this was assembled on
has no Foundry toolchain, so reproduce with the commands below rather than taking the number on
trust.

**Deployed on Arc Testnet, but not wired into AgentMesh.** The Arc rail that ships settles x402
payments against USDC's own EIP-3009 interface and needs no contract (see
[`../README.md`](../README.md)); nothing in that rail calls this.

Verified directly against `rpc.testnet.arc.io`:

| | |
|---|---|
| Address | [`0xb6F60f98aA3b36FbFf1f061d2f9C069Bd5Cb5748`](https://explorer.testnet.arc.io/address/0xb6F60f98aA3b36FbFf1f061d2f9C069Bd5Cb5748) — 7,108 bytes of code |
| Deploy | `0x04e8ef64bfc13fa879840ed0c161678be7d9bf168cf312ea728e1e8243785bd4`, block 71064852 |
| Exercised | `openRun 0x8c1a2510…b5b2e5` → `settleCall 0xd9104d3d…71aec0` → `closeRun 0x81a708dd…e970a4b`, all succeeded |
| Receipt proof | The exercised `settleCall` moved 0.02 USDC to the payee and 0.0002 to the fee recipient (100 bps as deployed), and emitted one log with 2 indexed and 4 data words — matching `CallSettled(bytes32,address,uint256,uint256,bytes32,uint256)` |

A live deployment is not the same as being in the submission's path. It is reachable and working;
it is simply not what settles the x402 calls.
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
