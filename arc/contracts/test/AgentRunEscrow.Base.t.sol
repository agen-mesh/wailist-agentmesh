// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {MockUSDC} from "./mocks/Tokens.sol";
import {FaithfulPermit2} from "./mocks/Permit2Mocks.sol";

/// @dev Shared fixtures/helpers. Contains NO test functions.
abstract contract EscrowTestBase is Test {
    // Events re-declared locally (identical signatures to the contract's).
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

    uint16 internal constant FEE_BPS = 100; // 1%
    uint256 internal constant BUDGET = 1_000e6;

    MockUSDC internal usdc;
    FaithfulPermit2 internal permit2;
    AgentRunEscrow internal escrow;

    address internal owner = makeAddr("owner");
    address internal settler = makeAddr("settler");
    address internal feeRecipient = makeAddr("feeRecipient");
    address internal agent = makeAddr("agent");
    address internal payee = makeAddr("payee");
    address internal payee2 = makeAddr("payee2");
    address internal eve = makeAddr("eve");
    address internal relayer = makeAddr("relayer");

    uint64 internal runDeadline;
    bytes32 internal rid;

    function setUp() public virtual {
        vm.warp(1_700_000_000);
        usdc = new MockUSDC();
        permit2 = new FaithfulPermit2();
        escrow = new AgentRunEscrow(address(usdc), address(permit2), owner, settler, feeRecipient, FEE_BPS);
        usdc.mint(agent, 1_000_000e6);
        vm.prank(agent);
        usdc.approve(address(escrow), type(uint256).max);
        runDeadline = uint64(block.timestamp + 1 days);
        rid = runIdOf(agent, 1);
    }

    function runIdOf(address a, uint96 tag) internal pure returns (bytes32) {
        return bytes32(abi.encodePacked(a, bytes12(tag)));
    }

    /// @dev Agent opens `rid` with BUDGET and the default deadline.
    function _open() internal {
        vm.prank(agent);
        escrow.openRun(rid, BUDGET, runDeadline);
    }

    /// @dev `a` (funded + approved on demand) opens runIdOf(a, tag).
    function _openFor(address a, uint96 tag, uint256 budget, uint64 dl) internal returns (bytes32 id) {
        id = runIdOf(a, tag);
        usdc.mint(a, budget);
        vm.startPrank(a);
        usdc.approve(address(escrow), type(uint256).max);
        escrow.openRun(id, budget, dl);
        vm.stopPrank();
    }

    function _settle(bytes32 id, address to, uint256 amount) internal {
        vm.prank(settler);
        escrow.settleCall(id, to, amount, bytes32("ref"));
    }

    function _spent(bytes32 id) internal view returns (uint256 s) {
        (,, s,,,) = escrow.runs(id);
    }

    function _newEscrow(address token, address permit, uint16 fee) internal returns (AgentRunEscrow e) {
        e = new AgentRunEscrow(token, permit, owner, settler, feeRecipient, fee);
    }

    function _call(address to, uint256 amount, bytes32 ref) internal pure returns (AgentRunEscrow.Call memory c) {
        c = AgentRunEscrow.Call({payee: to, amount: amount, toolRef: ref});
    }
}
