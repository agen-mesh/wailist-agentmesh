// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";

contract AgentRunEscrowBatchTest is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    function _batch(uint256 n, uint256 amount) internal view returns (AgentRunEscrow.Call[] memory cs) {
        cs = new AgentRunEscrow.Call[](n);
        for (uint256 i; i < n; ++i) {
            cs[i] = _call(i % 2 == 0 ? payee : payee2, amount, bytes32(i + 1));
        }
    }

    function _settleBatch(bytes32 id, AgentRunEscrow.Call[] memory cs) internal {
        vm.prank(settler);
        escrow.settleCallBatch(id, cs);
    }

    /// 10% fee, budget 1100e6 -> a 1000e6 payment exactly fills it.
    function _openTight() internal returns (bytes32 id) {
        vm.prank(owner);
        escrow.setFeeBps(1000);
        id = _openFor(makeAddr("tight"), 1, 1100e6, runDeadline);
    }

    function test_Batch_HappyPath_EventsBalancesRunningSpent() public {
        _open();
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](3);
        cs[0] = _call(payee, 100e6, bytes32("a"));
        cs[1] = _call(payee2, 200e6, bytes32("b"));
        cs[2] = _call(payee, 50e6, bytes32("c"));

        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee, 100e6, 1e6, bytes32("a"), 101e6);
        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee2, 200e6, 2e6, bytes32("b"), 303e6);
        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee, 50e6, 0.5e6, bytes32("c"), 353.5e6);
        _settleBatch(rid, cs);

        assertEq(usdc.balanceOf(payee), 150e6);
        assertEq(usdc.balanceOf(payee2), 200e6);
        assertEq(usdc.balanceOf(feeRecipient), 3.5e6);
        assertEq(_spent(rid), 353.5e6);
        assertEq(usdc.balanceOf(address(escrow)), BUDGET - 353.5e6);
    }

    function test_Batch_FeeRoundedPerItem() public {
        vm.prank(owner);
        escrow.setFeeBps(1000);
        bytes32 id = _openFor(makeAddr("r"), 1, BUDGET, runDeadline);
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](2);
        cs[0] = _call(payee, 15, REF); // fee 1 (1.5 floor)
        cs[1] = _call(payee2, 15, REF); // fee 1
        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(id, payee, 15, 1, REF, 16);
        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(id, payee2, 15, 1, REF, 32);
        _settleBatch(id, cs);
        assertEq(usdc.balanceOf(feeRecipient), 2, "per-item floor, not floor of aggregate (3)");
        assertEq(_spent(id), 32);
    }

    function test_Batch_SizeBounds() public {
        _open();
        AgentRunEscrow.Call[] memory empty = new AgentRunEscrow.Call[](0);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.InvalidBatchSize.selector, uint256(0)));
        escrow.settleCallBatch(rid, empty);

        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.InvalidBatchSize.selector, uint256(51)));
        escrow.settleCallBatch(rid, _batch(51, 1e6));
        assertEq(_spent(rid), 0);

        _settleBatch(rid, _batch(50, 1e6));
        assertEq(_spent(rid), 50 * (1e6 + 1e4));
        assertEq(usdc.balanceOf(payee) + usdc.balanceOf(payee2), 50e6);
        assertEq(usdc.balanceOf(feeRecipient), 50 * 1e4);

        // single-item batch also fine
        _settleBatch(rid, _batch(1, 1e6));
        assertEq(_spent(rid), 51 * (1e6 + 1e4));
    }

    function test_Batch_PartialOverspendRevertsWholeBatch() public {
        bytes32 id = _openTight();
        uint256 escrowBal = usdc.balanceOf(address(escrow));
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](3);
        cs[0] = _call(payee, 500e6, REF); // 550
        cs[1] = _call(payee2, 400e6, REF); // +440 = 990 (fits)
        cs[2] = _call(payee, 200e6, REF); // +220 = 1210 > 1100

        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.BudgetExceeded.selector, id));
        escrow.settleCallBatch(id, cs);

        assertEq(_spent(id), 0, "spent unchanged");
        assertTrue(escrow.isRunOpen(id));
        assertEq(usdc.balanceOf(address(escrow)), escrowBal, "escrow balance unchanged");
        assertEq(usdc.balanceOf(payee), 0);
        assertEq(usdc.balanceOf(payee2), 0);
        assertEq(usdc.balanceOf(feeRecipient), 0);
    }

    function test_Batch_PartialOverspendAfterPriorSpend() public {
        bytes32 id = _openTight();
        _settle(id, payee, 100e6); // spent 110
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](2);
        cs[0] = _call(payee, 400e6, REF); // +440 = 550
        cs[1] = _call(payee2, 500e6 + 1, REF); // fee 50e6 -> +550e6+1 = 1100e6+1 > budget
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.BudgetExceeded.selector, id));
        escrow.settleCallBatch(id, cs);
        assertEq(_spent(id), 110e6);
        assertEq(usdc.balanceOf(payee), 100e6);
        assertEq(usdc.balanceOf(payee2), 0);

        // exact fit succeeds
        cs[1] = _call(payee2, 500e6, REF);
        _settleBatch(id, cs);
        assertEq(_spent(id), 1100e6);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }

    function test_Batch_InvalidItemRevertsWholeBatch() public {
        _open();
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](3);
        cs[0] = _call(payee, 1e6, REF);
        cs[1] = _call(payee2, 0, REF);
        cs[2] = _call(payee, 1e6, REF);
        vm.prank(settler);
        vm.expectRevert(AgentRunEscrow.InvalidAmount.selector);
        escrow.settleCallBatch(rid, cs);

        cs[1] = _call(address(0), 1e6, REF);
        vm.prank(settler);
        vm.expectRevert(AgentRunEscrow.InvalidPayee.selector);
        escrow.settleCallBatch(rid, cs);

        cs[1] = _call(address(escrow), 1e6, REF);
        vm.prank(settler);
        vm.expectRevert(AgentRunEscrow.InvalidPayee.selector);
        escrow.settleCallBatch(rid, cs);

        assertEq(_spent(rid), 0);
        assertEq(usdc.balanceOf(payee), 0);
    }

    function test_Batch_UnauthorizedRejected() public {
        _open();
        address[3] memory bad = [agent, owner, eve];
        for (uint256 i; i < bad.length; ++i) {
            vm.prank(bad[i]);
            vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedSettler.selector, bad[i]));
            escrow.settleCallBatch(rid, _batch(1, 1e6));
        }
    }

    function test_Batch_DeadlineSemantics() public {
        _open();
        vm.warp(runDeadline);
        _settleBatch(rid, _batch(2, 1e6)); // exactly at deadline ok
        vm.warp(uint256(runDeadline) + 1);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunExpired.selector, rid));
        escrow.settleCallBatch(rid, _batch(2, 1e6));
    }

    function test_Batch_UnknownAndClosedRun() public {
        bytes32 ghost = runIdOf(agent, 42);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotFound.selector, ghost));
        escrow.settleCallBatch(ghost, _batch(1, 1e6));

        _open();
        vm.prank(agent);
        escrow.closeRun(rid);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotOpen.selector, rid));
        escrow.settleCallBatch(rid, _batch(1, 1e6));
    }

    function test_Batch_ThenCloseRefundsRemainder() public {
        _open();
        _settleBatch(rid, _batch(4, 10e6)); // spent 4 * 10.1 = 40.4
        uint256 before_ = usdc.balanceOf(agent);
        vm.prank(settler);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent) - before_, BUDGET - 40.4e6);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }
}
