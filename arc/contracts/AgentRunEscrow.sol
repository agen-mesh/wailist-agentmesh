// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {Ownable2Step} from "@openzeppelin/contracts/access/Ownable2Step.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/// @notice Minimal Permit2 SignatureTransfer interface; struct layout matches Permit2.
interface IPermit2 {
    struct TokenPermissions {
        address token;
        uint256 amount;
    }

    struct PermitTransferFrom {
        TokenPermissions permitted;
        uint256 nonce;
        uint256 deadline;
    }

    struct SignatureTransferDetails {
        address to;
        uint256 requestedAmount;
    }

    function permitWitnessTransferFrom(
        PermitTransferFrom calldata permit,
        SignatureTransferDetails calldata transferDetails,
        address owner,
        bytes32 witness,
        string calldata witnessTypeString,
        bytes calldata signature
    ) external;
}

/**
 * @title AgentRunEscrow
 * @notice Per-run USDC budgets and atomic per-call payments with event-based receipts.
 * @dev Intended for Arc Testnet (chain 5042002). All amounts are 6-decimal ERC-20
 * USDC units. Native USDC's 18-decimal scale is NEVER used. Token and Permit2
 * addresses are constructor configuration; no native-currency or payable paths exist.
 * Uses OpenZeppelin 5.6.1; project configuration selects Solidity 0.8.28 / Cancun.
 *
 * Threat model: the settler is trusted to select genuine calls, payees and toolRef
 * values; it can spend any open budget within the deadline, withhold service, or
 * close a run early (always refunding its agent). toolRef is an opaque receipt,
 * not a deduplication key or proof of API execution. An agent can close at ANY time;
 * transaction ordering can still place a valid settlement before that close.
 * Settlements are allowed at the deadline, but never after it. Only agent or the
 * current settler can close, even after expiry. Fee rates are snapshotted when runs
 * open; fee changes apply only to runs opened afterward. The fee recipient and
 * settler remain live/global, but admin changes never affect the agent's right to refund.
 *
 * runId layout: agent address (20 bytes) || 12-byte backend tag. Only the agent
 * (openRun) or its Permit2 signature can open IDs in its namespace, preventing
 * third-party squatting. The Permit2 witness already binds both runId and agent.
 * Tags are per-agent namespaced; 96-bit tag collisions across agents are irrelevant.
 *
 * A blocked payee or fee recipient makes the entire settlement transaction revert;
 * no failed payments are skipped. Closing never depends on either recipient or on
 * current fee settings. A blocked agent may be unable to receive its refund;
 * closing then reverts, leaving the run and its funds intact until the token allows
 * the transfer. A token freeze/blocklist affecting this escrow also prevents transfers.
 *
 * Accounting invariant: accounted escrow liabilities equal the sum of
 * (budget - spent) over open runs. With supported USDC and no unsolicited transfers
 * or fees directed back to this escrow, its ERC-20 balance equals that sum after
 * each successful transaction. Unsolicited tokens or self-directed fees create
 * surplus, so unconditional balance equality cannot be enforced by an ERC-20
 * recipient; solvency is balance >= liabilities. No sweep/rescue or owner withdrawal
 * exists. Fee-on-transfer and rebasing tokens are unsupported. Funding verifies the
 * exact balance increase. Only funding, settlement and refund paths move run funds.
 */
contract AgentRunEscrow is Ownable2Step, ReentrancyGuard {
    using SafeERC20 for IERC20;

    struct Run {
        address agent;
        uint256 budget;
        uint256 spent;
        uint64 deadline;
        bool open;
        uint16 feeBps;
    }

    struct Call {
        address payee;
        uint256 amount;
        bytes32 toolRef;
    }

    uint16 public constant MAX_FEE_BPS = 1000;
    uint256 public constant MAX_BATCH = 50;
    uint256 private constant BPS_DENOMINATOR = 10_000;
    bytes32 public constant RUN_WITNESS_TYPEHASH =
        keccak256("RunWitness(bytes32 runId,address agent,uint256 budget,uint64 deadline)");
    string public constant WITNESS_TYPE_STRING =
        "RunWitness witness)RunWitness(bytes32 runId,address agent,uint256 budget,uint64 deadline)TokenPermissions(address token,uint256 amount)";

    IERC20 public immutable usdc;
    IPermit2 public immutable permit2;
    address public settler;
    address public feeRecipient;
    uint16 public feeBps;
    mapping(bytes32 => Run) public runs;

    error ZeroAddress();
    error RunIdAgentMismatch(bytes32 runId, address agent);
    error RunAlreadyExists(bytes32 runId);
    error RunNotFound(bytes32 runId);
    error RunNotOpen(bytes32 runId);
    error InvalidBudget();
    error InvalidDeadline();
    error RunExpired(bytes32 runId);
    error InvalidAmount();
    error InvalidPayee();
    error FeeTooHigh(uint16 feeBps);
    error UnauthorizedSettler(address caller);
    error UnauthorizedClose(bytes32 runId, address caller);
    error BudgetExceeded(bytes32 runId);
    error InvalidBatchSize(uint256 size);
    error FundingAmountMismatch(uint256 expected, uint256 received);
    error RenounceOwnershipDisabled();

    event RunOpened(bytes32 indexed runId, address indexed agent, uint256 budget, uint64 deadline);
    event CallSettled(
        bytes32 indexed runId,
        address indexed payee,
        uint256 amount,
        uint256 fee,
        bytes32 toolRef,
        uint256 spentTotal
    );
    event RunClosed(bytes32 indexed runId, uint256 refunded, uint256 spent);
    event FeeBpsUpdated(uint16 oldValue, uint16 newValue);
    event FeeRecipientUpdated(address indexed oldValue, address indexed newValue);
    event SettlerUpdated(address indexed oldValue, address indexed newValue);

    modifier onlySettler() {
        if (msg.sender != settler) revert UnauthorizedSettler(msg.sender);
        _;
    }

    /// @notice Configures immutable ERC-20/Permit2 endpoints and initial administration.
    /// @dev Ownable checks the owner for zero; all other addresses are checked below.
    constructor(
        address usdc_,
        address permit2_,
        address owner_,
        address settler_,
        address feeRecipient_,
        uint16 feeBps_
    ) Ownable(owner_) {
        if (
            usdc_ == address(0) || permit2_ == address(0)
                || settler_ == address(0) || feeRecipient_ == address(0)
        ) revert ZeroAddress();
        if (feeBps_ > MAX_FEE_BPS) revert FeeTooHigh(feeBps_);
        usdc = IERC20(usdc_);
        permit2 = IPermit2(permit2_);
        settler = settler_;
        feeRecipient = feeRecipient_;
        feeBps = feeBps_;
    }

    /// @notice Updates the fee for runs opened afterward; existing runs retain their snapshot.
    function setFeeBps(uint16 newFeeBps) external nonReentrant onlyOwner {
        if (newFeeBps > MAX_FEE_BPS) revert FeeTooHigh(newFeeBps);
        uint16 previous = feeBps;
        feeBps = newFeeBps;
        emit FeeBpsUpdated(previous, newFeeBps);
    }

    /// @notice Updates the fee recipient without changing any refund destination.
    function setFeeRecipient(address newFeeRecipient) external nonReentrant onlyOwner {
        if (newFeeRecipient == address(0)) revert ZeroAddress();
        address previous = feeRecipient;
        feeRecipient = newFeeRecipient;
        emit FeeRecipientUpdated(previous, newFeeRecipient);
    }

    /// @notice Replaces the settler; every run's agent retains its own closing right.
    function setSettler(address newSettler) external nonReentrant onlyOwner {
        if (newSettler == address(0)) revert ZeroAddress();
        address previous = settler;
        settler = newSettler;
        emit SettlerUpdated(previous, newSettler);
    }

    /// @notice Starts two-step ownership transfer; zero cancels a pending transfer.
    function transferOwnership(address newOwner) public override nonReentrant onlyOwner {
        super.transferOwnership(newOwner);
    }

    /// @notice Only the pending owner can complete an ownership transfer.
    function acceptOwnership() public override nonReentrant {
        super.acceptOwnership();
    }

    /// @notice Ownership renunciation is disabled to preserve administration.
    function renounceOwnership() public override nonReentrant onlyOwner {
        revert RenounceOwnershipDisabled();
    }

    /// @notice Opens a unique run using the caller's prior ERC-20 allowance.
    /// @dev Only the agent can open IDs in its namespace here, preventing third-party squatting.
    /// The Permit2 alternative requires its signature; its witness already binds runId and agent.
    /// @param runId Agent address (20 bytes) || 12-byte backend tag; can never be reused.
    /// @param budget Funding in 6-decimal ERC-20 units.
    /// @param deadline Last timestamp at which settlement is permitted (strictly future).
    function openRun(bytes32 runId, uint256 budget, uint64 deadline) external nonReentrant {
        _recordRun(msg.sender, runId, budget, deadline);
        uint256 balanceBefore = usdc.balanceOf(address(this));
        usdc.safeTransferFrom(msg.sender, address(this), budget);
        _checkFunding(balanceBefore, budget);
        emit RunOpened(runId, msg.sender, budget, deadline);
    }

    /**
     * @notice Anyone can relay an agent's witness-bound Permit2 funding authorization.
     * @dev The agent must previously have approved USDC to Permit2. No ERC-20 approval
     * to this escrow is needed. Permit2 enforces signature validity, nonce uniqueness,
     * permit expiry and spender binding to this contract. The witness binds the agent,
     * run ID, budget and run deadline. A copied transaction can only open that same
     * run for that same agent, not redirect or change its funding. Signature expiry
     * (permitDeadline) is distinct from the run's settlement deadline.
     * runId layout: agent address (20 bytes) || 12-byte backend tag. Only the agent
     * or its Permit2 signature can open IDs in its namespace, preventing third-party
     * squatting; the witness already binds runId and agent.
     */
    function openRunWithPermit2(
        address agent,
        bytes32 runId,
        uint256 budget,
        uint64 deadline,
        uint256 permitNonce,
        uint256 permitDeadline,
        bytes calldata signature
    ) external nonReentrant {
        _recordRun(agent, runId, budget, deadline);
        IPermit2.PermitTransferFrom memory permit = IPermit2.PermitTransferFrom({
            permitted: IPermit2.TokenPermissions({token: address(usdc), amount: budget}),
            nonce: permitNonce,
            deadline: permitDeadline
        });
        IPermit2.SignatureTransferDetails memory details = IPermit2.SignatureTransferDetails({
            to: address(this), requestedAmount: budget
        });
        bytes32 witness = keccak256(abi.encode(RUN_WITNESS_TYPEHASH, runId, agent, budget, deadline));
        uint256 balanceBefore = usdc.balanceOf(address(this));
        permit2.permitWitnessTransferFrom(
            permit, details, agent, witness, WITNESS_TYPE_STRING, signature
        );
        _checkFunding(balanceBefore, budget);
        emit RunOpened(runId, agent, budget, deadline);
    }

    /**
     * @notice Pays one API call and its fee from a run, recording a receipt.
     * @dev Reverts when block.timestamp > deadline: after expiry the settler cannot
     * race an agent's reclamation. Uses the run's snapshotted fee rate; fee changes
     * apply only to runs opened afterward, while feeRecipient remains live/global.
     * Fees round down per call. Any failed transfer
     * reverts this entire settlement, restoring spent and leaving the run closable.
     */
    function settleCall(bytes32 runId, address payee, uint256 amount, bytes32 toolRef)
        external
        nonReentrant
        onlySettler
    {
        Run storage run = _settleableRun(runId);
        uint16 rate = run.feeBps;
        address recipient = feeRecipient;
        uint256 fee = _callFee(payee, amount, rate);
        uint256 spentTotal = run.spent + amount + fee;
        if (spentTotal > run.budget) revert BudgetExceeded(runId);
        run.spent = spentTotal;
        usdc.safeTransfer(payee, amount);
        if (fee > 0) usdc.safeTransfer(recipient, fee);
        emit CallSettled(runId, payee, amount, fee, toolRef, spentTotal);
    }

    /**
     * @notice Atomically settles 1..50 calls against one run.
     * @dev Same deadline policy as settleCall: block.timestamp > deadline reverts.
     * Uses the run's snapshotted fee rate; fee changes apply only to runs opened
     * afterward, while feeRecipient remains live/global.
     * All items are validated and final spent is stored before any transfer. Per-item
     * fees are rounded individually, then transferred once in aggregate. A failure
     * anywhere rolls back all transfers, accounting and logs; there is no skipping.
     */
    function settleCallBatch(bytes32 runId, Call[] calldata calls)
        external
        nonReentrant
        onlySettler
    {
        uint256 length = calls.length;
        if (length == 0 || length > MAX_BATCH) revert InvalidBatchSize(length);
        Run storage run = _settleableRun(runId);
        uint16 rate = run.feeBps;
        address recipient = feeRecipient;
        uint256 runningSpent = run.spent;
        uint256 finalSpent = runningSpent;
        uint256 totalFee = 0;
        uint256[] memory fees = new uint256[](length);
        for (uint256 i = 0; i < length; ++i) {
            Call calldata item = calls[i];
            uint256 fee = _callFee(item.payee, item.amount, rate);
            fees[i] = fee;
            totalFee += fee;
            finalSpent += item.amount + fee;
            if (finalSpent > run.budget) revert BudgetExceeded(runId);
        }
        run.spent = finalSpent;
        for (uint256 i = 0; i < length; ++i) {
            Call calldata item = calls[i];
            runningSpent += item.amount + fees[i];
            usdc.safeTransfer(item.payee, item.amount);
            emit CallSettled(runId, item.payee, item.amount, fees[i], item.toolRef, runningSpent);
        }
        if (totalFee > 0) usdc.safeTransfer(recipient, totalFee);
    }

    /**
     * @notice Agent or current settler may close at any time, always refunding agent.
     * @dev Expiry does not grant third parties a closing right. Neither the fee
     * recipient nor any payee is consulted. If the agent is blocked by USDC, a failed
     * refund reverts the close and keeps funds accounted for in the still-open run.
     */
    function closeRun(bytes32 runId) external nonReentrant {
        Run storage run = _openRun(runId);
        if (msg.sender != run.agent && msg.sender != settler) revert UnauthorizedClose(runId, msg.sender);
        uint256 spent = run.spent;
        uint256 refunded = run.budget - spent;
        address agent = run.agent;
        run.open = false;
        if (refunded > 0) usdc.safeTransfer(agent, refunded);
        emit RunClosed(runId, refunded, spent);
    }

    /// @notice Returns unspent funding for an open run, or zero if absent/closed.
    function remainingBudget(bytes32 runId) external view returns (uint256) {
        Run storage run = runs[runId];
        return run.open ? run.budget - run.spent : 0;
    }

    /// @notice Returns false for absent or closed runs.
    function isRunOpen(bytes32 runId) external view returns (bool) {
        return runs[runId].open;
    }

    function _recordRun(address agent, bytes32 runId, uint256 budget, uint64 deadline) private {
        if (agent == address(0)) revert ZeroAddress();
        if (bytes20(runId) != bytes20(agent)) revert RunIdAgentMismatch(runId, agent);
        if (runs[runId].agent != address(0)) revert RunAlreadyExists(runId);
        if (budget == 0) revert InvalidBudget();
        if (deadline <= block.timestamp) revert InvalidDeadline();
        runs[runId] = Run({agent: agent, budget: budget, spent: 0, deadline: deadline, open: true, feeBps: feeBps});
    }

    function _openRun(bytes32 runId) private view returns (Run storage run) {
        run = runs[runId];
        if (run.agent == address(0)) revert RunNotFound(runId);
        if (!run.open) revert RunNotOpen(runId);
    }

    function _settleableRun(bytes32 runId) private view returns (Run storage run) {
        run = _openRun(runId);
        if (block.timestamp > run.deadline) revert RunExpired(runId);
    }

    function _callFee(address payee, uint256 amount, uint16 rate) private view returns (uint256) {
        if (amount == 0) revert InvalidAmount();
        if (payee == address(0) || payee == address(this)) revert InvalidPayee();
        return amount * uint256(rate) / BPS_DENOMINATOR;
    }

    function _checkFunding(uint256 balanceBefore, uint256 budget) private view {
        uint256 balanceAfter = usdc.balanceOf(address(this));
        uint256 received = balanceAfter < balanceBefore ? 0 : balanceAfter - balanceBefore;
        if (balanceAfter < balanceBefore || received != budget) {
            revert FundingAmountMismatch(budget, received);
        }
    }
}
