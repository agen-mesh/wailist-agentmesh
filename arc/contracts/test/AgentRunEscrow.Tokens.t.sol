// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";
import {MockUSDC, FalseReturnUSDC, FeeOnTransferUSDC, HookUSDC, ReentrantActor} from "./mocks/Tokens.sol";

/// Hostile-token / hostile-recipient behaviour: blocklists, false returns, fee-on-transfer, reentrancy.
contract AgentRunEscrowTokensTest is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    // ───────────────────────── blocklisted payee ─────────────────────────

    function test_Blocklist_PayeeRevertsOnlyForThatPayee() public {
        _open();
        usdc.setBlocked(payee, true);

        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, payee));
        escrow.settleCall(rid, payee, 10e6, REF);
        assertEq(_spent(rid), 0, "spent rolled back");
        assertEq(usdc.balanceOf(address(escrow)), BUDGET);

        // other payees still work
        _settle(rid, payee2, 10e6);
        assertEq(usdc.balanceOf(payee2), 10e6);
        assertEq(_spent(rid), 10.1e6);

        // a batch containing the blocked payee reverts entirely (payee2 not paid either)
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](3);
        cs[0] = _call(payee2, 5e6, REF);
        cs[1] = _call(payee, 5e6, REF);
        cs[2] = _call(payee2, 5e6, REF);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, payee));
        escrow.settleCallBatch(rid, cs);
        assertEq(_spent(rid), 10.1e6);
        assertEq(usdc.balanceOf(payee2), 10e6);

        // run can still be closed and the agent refunded
        uint256 before_ = usdc.balanceOf(agent);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent) - before_, BUDGET - 10.1e6);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }

    // ───────────────────────── blocklisted fee recipient ─────────────────────────

    function test_Blocklist_FeeRecipientBlocksSettleButNotClose() public {
        _open();
        usdc.setBlocked(feeRecipient, true);

        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, feeRecipient));
        escrow.settleCall(rid, payee, 10e6, REF);

        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](1);
        cs[0] = _call(payee, 10e6, REF);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, feeRecipient));
        escrow.settleCallBatch(rid, cs);
        assertEq(_spent(rid), 0);
        assertEq(usdc.balanceOf(payee), 0);

        uint256 before_ = usdc.balanceOf(agent);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent) - before_, BUDGET, "full refund despite blocked fee recipient");
    }

    function test_Blocklist_OwnerCanReplaceFeeRecipientToUnblock() public {
        _open();
        usdc.setBlocked(feeRecipient, true);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, feeRecipient));
        escrow.settleCall(rid, payee, 10e6, REF);

        address nr = makeAddr("nr");
        vm.prank(owner);
        escrow.setFeeRecipient(nr);
        _settle(rid, payee, 10e6);
        assertEq(usdc.balanceOf(nr), 0.1e6);
        assertEq(usdc.balanceOf(payee), 10e6);
    }

    function test_Blocklist_ZeroFeeRunUnaffectedByBlockedFeeRecipient() public {
        vm.prank(owner);
        escrow.setFeeBps(0);
        _open();
        usdc.setBlocked(feeRecipient, true);
        _settle(rid, payee, 10e6);
        assertEq(usdc.balanceOf(payee), 10e6);
    }

    // ───────────────────────── blocklisted agent (refund) ─────────────────────────

    function test_Blocklist_BlockedAgentCloseRevertsAndKeepsFunds() public {
        _open();
        usdc.setBlocked(agent, true);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, agent));
        escrow.closeRun(rid);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(MockUSDC.Blocked.selector, agent));
        escrow.closeRun(rid);
        assertTrue(escrow.isRunOpen(rid), "run intact");
        assertEq(usdc.balanceOf(address(escrow)), BUDGET);

        usdc.setBlocked(agent, false);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertFalse(escrow.isRunOpen(rid));
    }

    // ───────────────────────── false-returning token ─────────────────────────

    function _falseSetup() internal returns (FalseReturnUSDC f, AgentRunEscrow e) {
        f = new FalseReturnUSDC();
        e = _newEscrow(address(f), address(permit2), FEE_BPS);
        f.mint(agent, 10_000e6);
        vm.prank(agent);
        f.approve(address(e), type(uint256).max);
    }

    function test_FalseReturn_PayeeAndFeeTransferCaughtBySafeERC20() public {
        (FalseReturnUSDC f, AgentRunEscrow e) = _falseSetup();
        vm.prank(agent);
        e.openRun(rid, BUDGET, runDeadline);

        bytes memory err = abi.encodeWithSelector(SafeERC20.SafeERC20FailedOperation.selector, address(f));
        f.setFalseFor(payee, true);
        vm.prank(settler);
        vm.expectRevert(err);
        e.settleCall(rid, payee, 10e6, REF);

        f.setFalseFor(payee, false);
        f.setFalseFor(feeRecipient, true);
        vm.prank(settler);
        vm.expectRevert(err);
        e.settleCall(rid, payee, 10e6, REF);

        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](1);
        cs[0] = _call(payee, 10e6, REF);
        vm.prank(settler);
        vm.expectRevert(err);
        e.settleCallBatch(rid, cs);

        (,, uint256 spent,,,) = e.runs(rid);
        assertEq(spent, 0, "no accounting left behind by a false-returning transfer");
        assertEq(f.balanceOf(payee), 0);
    }

    function test_FalseReturn_RefundFalseRevertsCloseAndKeepsRunOpen() public {
        (FalseReturnUSDC f, AgentRunEscrow e) = _falseSetup();
        vm.prank(agent);
        e.openRun(rid, BUDGET, runDeadline);
        f.setFalseFor(agent, true);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(SafeERC20.SafeERC20FailedOperation.selector, address(f)));
        e.closeRun(rid);
        assertTrue(e.isRunOpen(rid));
        assertEq(f.balanceOf(address(e)), BUDGET);
    }

    function test_FalseReturn_FundingFalseRejected() public {
        (FalseReturnUSDC f, AgentRunEscrow e) = _falseSetup();
        f.setFalseFor(address(e), true);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(SafeERC20.SafeERC20FailedOperation.selector, address(f)));
        e.openRun(rid, BUDGET, runDeadline);
        assertFalse(e.isRunOpen(rid));
    }

    // ───────────────────────── fee-on-transfer token ─────────────────────────

    function test_FeeOnTransfer_FundingMismatchReverts() public {
        FeeOnTransferUSDC t = new FeeOnTransferUSDC();
        AgentRunEscrow e = _newEscrow(address(t), address(permit2), FEE_BPS);
        t.mint(agent, 10_000e6);
        vm.prank(agent);
        t.approve(address(e), type(uint256).max);

        vm.prank(agent);
        vm.expectRevert(
            abi.encodeWithSelector(AgentRunEscrow.FundingAmountMismatch.selector, BUDGET, BUDGET - BUDGET / 100)
        );
        e.openRun(rid, BUDGET, runDeadline);
        assertFalse(e.isRunOpen(rid));
        assertEq(t.balanceOf(address(e)), 0);

        // sanity: same token with the fee switched off funds normally
        t.setFeeOn(false);
        vm.prank(agent);
        e.openRun(rid, BUDGET, runDeadline);
        assertTrue(e.isRunOpen(rid));
    }

    // ───────────────────────── reentrancy ─────────────────────────

    HookUSDC internal hk;
    AgentRunEscrow internal e2;
    ReentrantActor internal actor;
    bytes32 internal aid;

    /// @dev actor is agent + settler of a fresh escrow over a hook token. Hook not armed yet.
    function _setupReentrant() internal {
        hk = new HookUSDC();
        e2 = _newEscrow(address(hk), address(permit2), FEE_BPS);
        actor = new ReentrantActor();
        vm.prank(owner);
        e2.setSettler(address(actor));
        aid = runIdOf(address(actor), 1);
        hk.mint(address(actor), 10_000e6);
        actor.exec(address(hk), abi.encodeCall(hk.approve, (address(e2), type(uint256).max)));
        actor.exec(address(e2), abi.encodeCall(e2.openRun, (aid, BUDGET, runDeadline)));
    }

    function _guardSel() internal pure returns (bytes4) {
        return ReentrancyGuard.ReentrancyGuardReentrantCall.selector;
    }

    function _settleAsActor() internal {
        vm.prank(address(actor));
        e2.settleCall(aid, address(actor), 10e6, REF);
    }

    function test_Reentrancy_SettleCallbackCannotReenterSettle() public {
        _setupReentrant();
        hk.setHook(address(actor), address(actor));
        actor.configure(address(e2), abi.encodeCall(e2.settleCall, (aid, address(actor), 1e6, REF)), true);
        vm.expectRevert(_guardSel());
        _settleAsActor();
        assertEq(actor.hits(), 0, "state rolled back");
        (,, uint256 spent,,,) = e2.runs(aid);
        assertEq(spent, 0);
    }

    function test_Reentrancy_SwallowedAttemptsFailAndOuterSettlesOnce() public {
        _setupReentrant();
        hk.setHook(address(actor), address(actor));
        bytes[3] memory attacks = [
            abi.encodeCall(e2.settleCall, (aid, address(actor), 1e6, REF)),
            abi.encodeCall(e2.closeRun, (aid)),
            abi.encodeCall(e2.openRun, (runIdOf(address(actor), 2), 1e6, runDeadline))
        ];
        for (uint256 i; i < attacks.length; ++i) {
            actor.configure(address(e2), attacks[i], false);
            _settleAsActor();
            assertFalse(actor.lastOk(), "reentrant call must fail");
            assertEq(bytes4(actor.lastRet()), _guardSel());
        }
        // batch variant
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](1);
        cs[0] = _call(address(actor), 1e6, REF);
        actor.configure(address(e2), abi.encodeCall(e2.settleCallBatch, (aid, cs)), false);
        _settleAsActor();
        assertFalse(actor.lastOk());
        assertEq(bytes4(actor.lastRet()), _guardSel());

        // 4 outer settles of 10e6 (+1% fee), nothing extra, run still open, balances conserved
        (,, uint256 spent,, bool open_,) = e2.runs(aid);
        assertEq(spent, 4 * 10.1e6); // 4 outer single settles of 10e6 (+1%); inner attempts all failed
        assertTrue(open_);
        assertEq(hk.balanceOf(address(e2)), BUDGET - spent);
        assertEq(actor.hits(), 4);
    }

    function test_Reentrancy_BatchCallbackCannotReenter() public {
        _setupReentrant();
        hk.setHook(address(actor), address(actor));
        actor.configure(address(e2), abi.encodeCall(e2.closeRun, (aid)), true);
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](2);
        cs[0] = _call(address(actor), 1e6, REF);
        cs[1] = _call(address(actor), 1e6, REF);
        vm.prank(address(actor));
        vm.expectRevert(_guardSel());
        e2.settleCallBatch(aid, cs);
        assertTrue(e2.isRunOpen(aid));
        assertEq(hk.balanceOf(address(e2)), BUDGET);
    }

    function test_Reentrancy_RefundCallbackCannotReenter() public {
        _setupReentrant();
        hk.setHook(address(actor), address(actor));
        // refund is delivered to the actor (agent): try to re-close / settle / re-open from inside
        bytes[3] memory attacks = [
            abi.encodeCall(e2.closeRun, (aid)),
            abi.encodeCall(e2.settleCall, (aid, address(actor), 1e6, REF)),
            abi.encodeCall(e2.openRun, (runIdOf(address(actor), 2), 1e6, runDeadline))
        ];
        for (uint256 i; i < attacks.length; ++i) {
            actor.configure(address(e2), attacks[i], true);
            vm.prank(address(actor));
            vm.expectRevert(_guardSel());
            e2.closeRun(aid);
            assertTrue(e2.isRunOpen(aid), "close rolled back");
        }
        // swallowed variant: close succeeds exactly once, refund paid exactly once
        actor.configure(address(e2), attacks[0], false);
        uint256 before_ = hk.balanceOf(address(actor));
        vm.prank(address(actor));
        e2.closeRun(aid);
        assertFalse(actor.lastOk());
        assertEq(hk.balanceOf(address(actor)) - before_, BUDGET);
        assertEq(hk.balanceOf(address(e2)), 0);
    }

    function test_Reentrancy_FundingCallbackCannotReenterOpenRun() public {
        _setupReentrant();
        // arm hook for transfers INTO the escrow (funding transferFrom)
        hk.setHook(address(e2), address(actor));
        bytes32 id2 = runIdOf(address(actor), 2);
        actor.configure(address(e2), abi.encodeCall(e2.openRun, (id2, 1e6, runDeadline)), true);
        vm.expectRevert(_guardSel());
        actor.exec(address(e2), abi.encodeCall(e2.openRun, (id2, 5e6, runDeadline)));
        assertFalse(e2.isRunOpen(id2));
    }
}
