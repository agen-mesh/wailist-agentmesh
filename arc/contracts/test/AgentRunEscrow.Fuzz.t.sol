// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";

contract AgentRunEscrowFuzzTest is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    function _freshRun(uint256 budget, uint16 fee) internal returns (bytes32 id) {
        vm.prank(owner);
        escrow.setFeeBps(fee);
        id = _openFor(makeAddr("fz"), 1, budget, runDeadline);
    }

    /// PROPERTY: over any sequence of settleCall, spent <= budget; a call succeeds iff
    /// spent + amount + floor(amount*fee/1e4) <= budget; escrow balance == budget - spent.
    function testFuzz_SpentNeverExceedsBudget(uint256 budgetSeed, uint256 feeSeed, uint256[8] memory seeds) public {
        uint256 budget = bound(budgetSeed, 1, 1e15);
        uint16 fee = uint16(bound(feeSeed, 0, 1000));
        bytes32 id = _freshRun(budget, fee);

        uint256 spent;
        for (uint256 i; i < seeds.length; ++i) {
            uint256 amount = bound(seeds[i], 1, budget / 4 + 1);
            uint256 expectedFee = amount * fee / 10_000;
            uint256 total = spent + amount + expectedFee;

            vm.prank(settler);
            try escrow.settleCall(id, payee, amount, REF) {
                assertLe(total, budget, "succeeded beyond budget");
                spent = total;
            } catch {
                assertGt(total, budget, "reverted although it fit");
            }
            assertEq(_spent(id), spent);
            assertLe(_spent(id), budget);
            assertEq(usdc.balanceOf(address(escrow)), budget - spent);
        }
        assertEq(usdc.balanceOf(payee) + usdc.balanceOf(feeRecipient), spent, "paid out == spent");

        // closing returns exactly the unspent remainder to the agent
        address a = makeAddr("fz");
        uint256 before_ = usdc.balanceOf(a);
        vm.prank(a);
        escrow.closeRun(id);
        assertEq(usdc.balanceOf(a) - before_, budget - spent);
        assertEq(usdc.balanceOf(address(escrow)), 0);
    }

    /// PROPERTY: a batch is all-or-nothing and succeeds iff the per-item-rounded total fits.
    function testFuzz_BatchIsAtomic(uint256 budgetSeed, uint256 feeSeed, uint256[6] memory seeds) public {
        uint256 budget = bound(budgetSeed, 1, 1e12);
        uint16 fee = uint16(bound(feeSeed, 0, 1000));
        bytes32 id = _freshRun(budget, fee);

        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](seeds.length);
        uint256 total;
        uint256 feeTotal;
        uint256 payTotal;
        for (uint256 i; i < seeds.length; ++i) {
            uint256 amount = bound(seeds[i], 1, budget / 3 + 1);
            uint256 f = amount * fee / 10_000;
            cs[i] = _call(i % 2 == 0 ? payee : payee2, amount, bytes32(i));
            total += amount + f;
            feeTotal += f;
            payTotal += amount;
        }

        vm.prank(settler);
        try escrow.settleCallBatch(id, cs) {
            assertLe(total, budget);
            assertEq(_spent(id), total);
            assertEq(usdc.balanceOf(payee) + usdc.balanceOf(payee2), payTotal);
            assertEq(usdc.balanceOf(feeRecipient), feeTotal);
            assertEq(usdc.balanceOf(address(escrow)), budget - total);
        } catch {
            assertGt(total, budget);
            assertEq(_spent(id), 0);
            assertEq(usdc.balanceOf(address(escrow)), budget);
            assertEq(usdc.balanceOf(payee) + usdc.balanceOf(payee2) + usdc.balanceOf(feeRecipient), 0);
        }
    }

    /// PROPERTY: fee == floor(amount * feeBps / 10000), paid to the fee recipient, on top of the payee amount.
    function testFuzz_FeeIsFlooredBps(uint256 amountSeed, uint256 feeSeed) public {
        uint256 amount = bound(amountSeed, 1, 1e18);
        uint16 fee = uint16(bound(feeSeed, 0, 1000));
        uint256 expectedFee = amount * fee / 10_000;
        bytes32 id = _freshRun(amount + expectedFee, fee);

        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(id, payee, amount, expectedFee, REF, amount + expectedFee);
        _settle(id, payee, amount);

        assertEq(usdc.balanceOf(payee), amount);
        assertEq(usdc.balanceOf(feeRecipient), expectedFee);
        assertEq(escrow.remainingBudget(id), 0);
        assertLe(expectedFee * 10_000, amount * fee);
        assertGt((expectedFee + 1) * 10_000, amount * fee);
    }

    /// PROPERTY: only an address equal to the runId's high 20 bytes can open it via openRun.
    function testFuzz_OpenRunRequiresAgentPrefix(address caller, bytes32 id) public {
        vm.assume(caller != address(0) && caller != address(escrow));
        vm.assume(bytes20(id) != bytes20(caller));
        vm.prank(caller);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunIdAgentMismatch.selector, id, caller));
        escrow.openRun(id, 1, runDeadline);
    }

    /// PROPERTY: settle works iff block.timestamp <= deadline.
    function testFuzz_SettleDeadlineBoundary(uint256 offsetSeed) public {
        _open();
        uint256 t = bound(offsetSeed, 0, 3 days);
        vm.warp(block.timestamp + t);
        vm.prank(settler);
        try escrow.settleCall(rid, payee, 1e6, REF) {
            assertLe(block.timestamp, runDeadline);
        } catch {
            assertGt(block.timestamp, runDeadline);
        }
    }

    /// PROPERTY: agent can always close and gets budget - spent regardless of time.
    function testFuzz_AgentCanAlwaysClose(uint256 offsetSeed, uint256 amountSeed) public {
        _open();
        uint256 amount = bound(amountSeed, 1, 500e6);
        _settle(rid, payee, amount);
        vm.warp(block.timestamp + bound(offsetSeed, 0, 3650 days));
        uint256 spent = _spent(rid);
        uint256 before_ = usdc.balanceOf(agent);
        vm.prank(agent);
        escrow.closeRun(rid);
        assertEq(usdc.balanceOf(agent) - before_, BUDGET - spent);
    }
}
