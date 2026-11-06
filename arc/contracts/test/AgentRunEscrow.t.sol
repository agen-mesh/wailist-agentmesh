// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";

contract AgentRunEscrowTest is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    // ───────────────────────── Deployment ─────────────────────────

    function test_Deploy_StoresConfig() public view {
        assertEq(address(escrow.usdc()), address(usdc));
        assertEq(address(escrow.permit2()), address(permit2));
        assertEq(escrow.owner(), owner);
        assertEq(escrow.settler(), settler);
        assertEq(escrow.feeRecipient(), feeRecipient);
        assertEq(escrow.feeBps(), FEE_BPS);
        assertEq(escrow.MAX_BATCH(), 50);
        assertEq(escrow.MAX_FEE_BPS(), 1000);
    }

    function test_Deploy_RevertsOnZeroAddresses() public {
        address u = address(usdc);
        address p = address(permit2);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        new AgentRunEscrow(address(0), p, owner, settler, feeRecipient, 0);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        new AgentRunEscrow(u, address(0), owner, settler, feeRecipient, 0);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        new AgentRunEscrow(u, p, owner, address(0), feeRecipient, 0);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        new AgentRunEscrow(u, p, owner, settler, address(0), 0);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableInvalidOwner.selector, address(0)));
        new AgentRunEscrow(u, p, address(0), settler, feeRecipient, 0);
    }

    function test_Deploy_RevertsOnFeeTooHigh() public {
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.FeeTooHigh.selector, uint16(1001)));
        new AgentRunEscrow(address(usdc), address(permit2), owner, settler, feeRecipient, 1001);
        // boundary accepted
        AgentRunEscrow e = new AgentRunEscrow(address(usdc), address(permit2), owner, settler, feeRecipient, 1000);
        assertEq(e.feeBps(), 1000);
    }

    // ───────────────────────── openRun ─────────────────────────

    function test_OpenRun_FundsEscrowAndEmits() public {
        uint256 before_ = usdc.balanceOf(agent);
        vm.expectEmit(true, true, false, true, address(escrow));
        emit RunOpened(rid, agent, BUDGET, runDeadline);
        _open();

        assertEq(usdc.balanceOf(address(escrow)), BUDGET);
        assertEq(usdc.balanceOf(agent), before_ - BUDGET);
        assertTrue(escrow.isRunOpen(rid));
        assertEq(escrow.remainingBudget(rid), BUDGET);
        (address a, uint256 b, uint256 s, uint64 d, bool o, uint16 f) = escrow.runs(rid);
        assertEq(a, agent);
        assertEq(b, BUDGET);
        assertEq(s, 0);
        assertEq(d, runDeadline);
        assertTrue(o);
        assertEq(f, FEE_BPS);
    }

    function test_OpenRun_RevertsWhenRunIdNotAgentPrefixed() public {
        // eve tries to squat agent's namespace
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunIdAgentMismatch.selector, rid, eve));
        escrow.openRun(rid, BUDGET, runDeadline);
        assertFalse(escrow.isRunOpen(rid));
    }

    function test_OpenRun_ZeroRunIdRejected() public {
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunIdAgentMismatch.selector, bytes32(0), agent));
        escrow.openRun(bytes32(0), BUDGET, runDeadline);
    }

    function test_OpenRun_AgentPrefixWithZeroTagAllowed() public {
        bytes32 id = runIdOf(agent, 0);
        vm.prank(agent);
        escrow.openRun(id, BUDGET, runDeadline);
        assertTrue(escrow.isRunOpen(id));
    }

    function test_OpenRun_ZeroBudgetRejected() public {
        vm.prank(agent);
        vm.expectRevert(AgentRunEscrow.InvalidBudget.selector);
        escrow.openRun(rid, 0, runDeadline);
    }

    function test_OpenRun_PastAndCurrentDeadlineRejected() public {
        vm.startPrank(agent);
        vm.expectRevert(AgentRunEscrow.InvalidDeadline.selector);
        escrow.openRun(rid, BUDGET, uint64(block.timestamp));
        vm.expectRevert(AgentRunEscrow.InvalidDeadline.selector);
        escrow.openRun(rid, BUDGET, uint64(block.timestamp - 1));
        vm.expectRevert(AgentRunEscrow.InvalidDeadline.selector);
        escrow.openRun(rid, BUDGET, 0);
        // next second is fine
        escrow.openRun(rid, BUDGET, uint64(block.timestamp + 1));
        vm.stopPrank();
        assertTrue(escrow.isRunOpen(rid));
    }

    function test_OpenRun_DuplicateIdRejected() public {
        _open();
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunAlreadyExists.selector, rid));
        escrow.openRun(rid, BUDGET, runDeadline);
        assertEq(usdc.balanceOf(address(escrow)), BUDGET);
    }

    function test_OpenRun_ClosedIdCannotBeReopened() public {
        _open();
        vm.prank(agent);
        escrow.closeRun(rid);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunAlreadyExists.selector, rid));
        escrow.openRun(rid, BUDGET, runDeadline);
    }

    function test_OpenRun_RevertsWithoutAllowanceOrBalance() public {
        address poor = makeAddr("poor");
        bytes32 id = runIdOf(poor, 7);
        vm.prank(poor);
        vm.expectRevert(); // ERC20InsufficientAllowance
        escrow.openRun(id, BUDGET, runDeadline);

        vm.startPrank(poor);
        usdc.approve(address(escrow), type(uint256).max);
        vm.expectRevert(); // ERC20InsufficientBalance
        escrow.openRun(id, BUDGET, runDeadline);
        vm.stopPrank();
        assertFalse(escrow.isRunOpen(id));
    }

    function test_OpenRun_FeeSnapshotUsesCurrentRate() public {
        vm.prank(owner);
        escrow.setFeeBps(777);
        _open();
        (,,,,, uint16 f) = escrow.runs(rid);
        assertEq(f, 777);
    }

    // ───────────────────────── settleCall happy / validation ─────────────────────────

    function test_HappyPath_OpenSettleClose_ExactBalancesAndEvents() public {
        uint256 agentStart = usdc.balanceOf(agent);
        _open();

        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee, 200e6, 2e6, REF, 202e6);
        _settle(rid, payee, 200e6);

        assertEq(usdc.balanceOf(payee), 200e6);
        assertEq(usdc.balanceOf(feeRecipient), 2e6);
        assertEq(usdc.balanceOf(address(escrow)), BUDGET - 202e6);
        assertEq(_spent(rid), 202e6);
        assertEq(escrow.remainingBudget(rid), BUDGET - 202e6);

        // second call: spentTotal is cumulative
        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee2, 100e6, 1e6, REF, 303e6);
        _settle(rid, payee2, 100e6);
        assertEq(usdc.balanceOf(payee2), 100e6);
        assertEq(usdc.balanceOf(feeRecipient), 3e6);

        vm.expectEmit(true, false, false, true, address(escrow));
        emit RunClosed(rid, BUDGET - 303e6, 303e6);
        vm.prank(agent);
        escrow.closeRun(rid);

        assertEq(usdc.balanceOf(agent), agentStart - 303e6);
        assertEq(usdc.balanceOf(address(escrow)), 0);
        assertFalse(escrow.isRunOpen(rid));
        assertEq(escrow.remainingBudget(rid), 0);
    }

    function test_Settle_UnauthorizedCallersRejected() public {
        _open();
        address[4] memory bad = [agent, owner, eve, feeRecipient];
        for (uint256 i; i < bad.length; ++i) {
            vm.prank(bad[i]);
            vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedSettler.selector, bad[i]));
            escrow.settleCall(rid, payee, 1e6, REF);
        }
        assertEq(_spent(rid), 0);
    }

    function test_Settle_InvalidInputsRejected() public {
        _open();
        vm.startPrank(settler);
        vm.expectRevert(AgentRunEscrow.InvalidAmount.selector);
        escrow.settleCall(rid, payee, 0, REF);
        vm.expectRevert(AgentRunEscrow.InvalidPayee.selector);
        escrow.settleCall(rid, address(0), 1e6, REF);
        vm.expectRevert(AgentRunEscrow.InvalidPayee.selector);
        escrow.settleCall(rid, address(escrow), 1e6, REF);
        vm.stopPrank();
        assertEq(_spent(rid), 0);
    }

    function test_Settle_UnknownAndClosedRun() public {
        bytes32 ghost = runIdOf(agent, 99);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotFound.selector, ghost));
        escrow.settleCall(ghost, payee, 1e6, REF);

        _open();
        vm.prank(agent);
        escrow.closeRun(rid);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotOpen.selector, rid));
        escrow.settleCall(rid, payee, 1e6, REF);
    }

    // ───────────────────────── fee math ─────────────────────────

    function test_FeeMath_SeveralRates() public {
        uint16[5] memory rates = [uint16(0), 1, 250, 999, 1000];
        uint256[5] memory amounts = [uint256(1), 9_999, 10_000, 12_345_678, 1_000_000_007];
        for (uint256 r; r < rates.length; ++r) {
            vm.prank(owner);
            escrow.setFeeBps(rates[r]);
            address a = makeAddr(string.concat("feeAgent", vm.toString(r)));
            bytes32 id = _openFor(a, 1, 100_000e6, runDeadline);
            uint256 cumulative;
            for (uint256 i; i < amounts.length; ++i) {
                uint256 expectedFee = amounts[i] * rates[r] / 10_000;
                uint256 feeBefore = usdc.balanceOf(feeRecipient);
                uint256 payeeBefore = usdc.balanceOf(payee);
                vm.expectEmit(true, true, false, true, address(escrow));
                cumulative += amounts[i] + expectedFee;
                emit CallSettled(id, payee, amounts[i], expectedFee, REF, cumulative);
                _settle(id, payee, amounts[i]);
                assertEq(usdc.balanceOf(feeRecipient) - feeBefore, expectedFee, "fee");
                assertEq(usdc.balanceOf(payee) - payeeBefore, amounts[i], "payee");
            }
            assertEq(_spent(id), cumulative);
        }
    }

    function test_FeeMath_RoundsDown() public {
        vm.prank(owner);
        escrow.setFeeBps(1);
        _open();
        _settle(rid, payee, 9_999); // 0.9999 -> 0
        assertEq(usdc.balanceOf(feeRecipient), 0);
        assertEq(_spent(rid), 9_999);
        _settle(rid, payee, 10_000); // exactly 1
        assertEq(usdc.balanceOf(feeRecipient), 1);

        vm.prank(owner);
        escrow.setFeeBps(1000);
        bytes32 id2 = _openFor(makeAddr("a2"), 1, BUDGET, runDeadline);
        _settle(id2, payee, 19); // 1.9 -> 1
        assertEq(usdc.balanceOf(feeRecipient), 2);
        _settle(id2, payee, 9); // 0.9 -> 0
        assertEq(usdc.balanceOf(feeRecipient), 2);
    }

    function test_FeeMath_ZeroFeeMakesNoFeeTransfer() public {
        vm.prank(owner);
        escrow.setFeeBps(0);
        _open();
        _settle(rid, payee, 500e6);
        assertEq(usdc.balanceOf(feeRecipient), 0);
        assertEq(_spent(rid), 500e6);
    }

    // ───────────────────────── overspend boundary ─────────────────────────

    function _openTenPercent(uint256 budget) internal returns (bytes32 id) {
        vm.prank(owner);
        escrow.setFeeBps(1000);
        id = _openFor(makeAddr("tenAgent"), 1, budget, runDeadline);
    }

    function test_Overspend_ExactBoundarySucceeds() public {
        bytes32 id = _openTenPercent(1100e6);
        _settle(id, payee, 1000e6); // 1000 + 100 fee == 1100
        assertEq(_spent(id), 1100e6);
        assertEq(escrow.remainingBudget(id), 0);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }

    function test_Overspend_OneOverReverts() public {
        bytes32 id = _openTenPercent(1100e6);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.BudgetExceeded.selector, id));
        escrow.settleCall(id, payee, 1000e6 + 1, REF); // fee floors to 100e6 -> total 1100e6+1
        assertEq(_spent(id), 0);
        assertEq(usdc.balanceOf(address(escrow)), 1100e6);
    }

    function test_Overspend_CumulativeBoundary() public {
        bytes32 id = _openTenPercent(1100e6);
        _settle(id, payee, 500e6); // 550 spent
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.BudgetExceeded.selector, id));
        escrow.settleCall(id, payee, 500e6 + 1, REF);
        _settle(id, payee, 500e6); // exactly fills
        assertEq(_spent(id), 1100e6);
    }

    function test_Overspend_ZeroFeeBoundary() public {
        vm.prank(owner);
        escrow.setFeeBps(0);
        _open();
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.BudgetExceeded.selector, rid));
        escrow.settleCall(rid, payee, BUDGET + 1, REF);
        _settle(rid, payee, BUDGET);
        assertEq(_spent(rid), BUDGET);
    }

    // ───────────────────────── deadline ─────────────────────────

    function test_Deadline_SettleAtDeadlineOkAfterRevert() public {
        _open();
        vm.warp(runDeadline);
        _settle(rid, payee, 1e6);
        assertEq(usdc.balanceOf(payee), 1e6);

        vm.warp(uint256(runDeadline) + 1);
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunExpired.selector, rid));
        escrow.settleCall(rid, payee, 1e6, REF);
    }

    // ───────────────────────── closeRun ─────────────────────────

    function test_Close_AgentBeforeDeadlineRefundsRemainder() public {
        uint256 start = usdc.balanceOf(agent);
        _open();
        _settle(rid, payee, 100e6); // spent 101e6
        vm.expectEmit(true, false, false, true, address(escrow));
        emit RunClosed(rid, BUDGET - 101e6, 101e6);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent), start - 101e6);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }

    function test_Close_AgentAfterDeadlineWithoutSettler() public {
        _open();
        vm.warp(uint256(runDeadline) + 30 days);
        uint256 before_ = usdc.balanceOf(agent);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent), before_ + BUDGET);
        assertFalse(escrow.isRunOpen(rid));
    }

    function test_Close_ThirdPartiesRejected() public {
        _open();
        address[4] memory bad = [eve, owner, feeRecipient, payee];
        for (uint256 i; i < bad.length; ++i) {
            vm.prank(bad[i]);
            vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedClose.selector, rid, bad[i]));
            escrow.closeRun(rid);
        }
        // also rejected after expiry
        vm.warp(uint256(runDeadline) + 1);
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedClose.selector, rid, eve));
        escrow.closeRun(rid);
        assertTrue(escrow.isRunOpen(rid));
    }

    function test_Close_BySettlerRefundsAgentNotSettler() public {
        _open();
        _settle(rid, payee, 100e6);
        uint256 agentBefore = usdc.balanceOf(agent);
        uint256 settlerBefore = usdc.balanceOf(settler);
        vm.prank(settler);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent), agentBefore + BUDGET - 101e6);
        assertEq(usdc.balanceOf(settler), settlerBefore);
    }

    function test_Close_FullySpentRunEmitsZeroRefund() public {
        vm.prank(owner);
        escrow.setFeeBps(0);
        _open();
        _settle(rid, payee, BUDGET);
        vm.expectEmit(true, false, false, true, address(escrow));
        emit RunClosed(rid, 0, BUDGET);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertFalse(escrow.isRunOpen(rid));
    }

    function test_Close_TwiceAndUnknown() public {
        _open();
        vm.prank(agent);
        escrow.closeRun(rid);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotOpen.selector, rid));
        escrow.closeRun(rid);

        bytes32 ghost = runIdOf(agent, 55);
        vm.prank(agent);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunNotFound.selector, ghost));
        escrow.closeRun(ghost);
    }

    function test_Close_ReplacedSettlerLosesRightAgentKeepsIt() public {
        _open();
        address newSettler = makeAddr("newSettler");
        vm.prank(owner);
        escrow.setSettler(newSettler);

        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedClose.selector, rid, settler));
        escrow.closeRun(rid);

        vm.prank(newSettler);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }
}
