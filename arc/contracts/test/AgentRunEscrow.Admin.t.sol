// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";

contract AgentRunEscrowAdminTest is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    // ───────────────────────── fee snapshot ─────────────────────────

    function test_FeeSnapshot_ChangeDoesNotAffectOpenRun() public {
        _open(); // snapshot 100 bps
        vm.prank(owner);
        escrow.setFeeBps(1000);

        vm.expectEmit(true, true, false, true, address(escrow));
        emit CallSettled(rid, payee, 100e6, 1e6, REF, 101e6);
        _settle(rid, payee, 100e6);
        assertEq(usdc.balanceOf(feeRecipient), 1e6);

        // a run opened afterwards uses the new rate
        bytes32 id2 = _openFor(makeAddr("agent2"), 1, BUDGET, runDeadline);
        _settle(id2, payee, 100e6);
        assertEq(usdc.balanceOf(feeRecipient), 1e6 + 10e6);

        // lowering to zero also leaves old runs untouched
        vm.prank(owner);
        escrow.setFeeBps(0);
        _settle(rid, payee, 100e6);
        assertEq(usdc.balanceOf(feeRecipient), 1e6 + 10e6 + 1e6);
    }

    function test_FeeSnapshot_AppliesToBatchToo() public {
        _open();
        vm.prank(owner);
        escrow.setFeeBps(1000);
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](2);
        cs[0] = _call(payee, 100e6, REF);
        cs[1] = _call(payee2, 100e6, REF);
        vm.prank(settler);
        escrow.settleCallBatch(rid, cs);
        assertEq(usdc.balanceOf(feeRecipient), 2e6);
        assertEq(_spent(rid), 202e6);
    }

    // ───────────────────────── setFeeBps ─────────────────────────

    function test_SetFeeBps_OwnerCanSetAndEmits() public {
        vm.expectEmit(false, false, false, true, address(escrow));
        emit FeeBpsUpdated(FEE_BPS, 1000);
        vm.prank(owner);
        escrow.setFeeBps(1000);
        assertEq(escrow.feeBps(), 1000);
        vm.prank(owner);
        escrow.setFeeBps(0);
        assertEq(escrow.feeBps(), 0);
    }

    function test_SetFeeBps_AboveMaxReverts() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.FeeTooHigh.selector, uint16(1001)));
        escrow.setFeeBps(1001);
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.FeeTooHigh.selector, type(uint16).max));
        escrow.setFeeBps(type(uint16).max);
        assertEq(escrow.feeBps(), FEE_BPS);
    }

    function test_SetFeeBps_OnlyOwner() public {
        address[3] memory bad = [eve, settler, agent];
        for (uint256 i; i < bad.length; ++i) {
            vm.prank(bad[i]);
            vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, bad[i]));
            escrow.setFeeBps(5);
        }
    }

    // ───────────────────────── setFeeRecipient / setSettler ─────────────────────────

    function test_SetFeeRecipient_OwnerOnlyNonZeroAndEmits() public {
        address nr = makeAddr("newRecipient");
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.setFeeRecipient(nr);

        vm.prank(owner);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        escrow.setFeeRecipient(address(0));

        vm.expectEmit(true, true, false, false, address(escrow));
        emit FeeRecipientUpdated(feeRecipient, nr);
        vm.prank(owner);
        escrow.setFeeRecipient(nr);
        assertEq(escrow.feeRecipient(), nr);

        _open();
        _settle(rid, payee, 100e6);
        assertEq(usdc.balanceOf(nr), 1e6);
        assertEq(usdc.balanceOf(feeRecipient), 0);
    }

    function test_SetSettler_OwnerOnlyNonZeroAndEmits() public {
        address ns = makeAddr("newSettler");
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.setSettler(ns);

        vm.prank(owner);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        escrow.setSettler(address(0));

        vm.expectEmit(true, true, false, false, address(escrow));
        emit SettlerUpdated(settler, ns);
        vm.prank(owner);
        escrow.setSettler(ns);
        assertEq(escrow.settler(), ns);

        _open();
        vm.prank(settler);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.UnauthorizedSettler.selector, settler));
        escrow.settleCall(rid, payee, 1e6, REF);
        vm.prank(ns);
        escrow.settleCall(rid, payee, 1e6, REF);
    }

    // ───────────────────────── Ownable2Step ─────────────────────────

    function test_Ownership_TwoStepTransfer() public {
        address newOwner = makeAddr("newOwner");
        vm.prank(owner);
        escrow.transferOwnership(newOwner);
        assertEq(escrow.owner(), owner, "owner unchanged until accept");
        assertEq(escrow.pendingOwner(), newOwner);

        // old owner keeps power until acceptance
        vm.prank(owner);
        escrow.setFeeBps(10);

        vm.prank(newOwner);
        escrow.acceptOwnership();
        assertEq(escrow.owner(), newOwner);
        assertEq(escrow.pendingOwner(), address(0));

        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, owner));
        escrow.setFeeBps(20);
        vm.prank(newOwner);
        escrow.setFeeBps(20);
        assertEq(escrow.feeBps(), 20);
    }

    function test_Ownership_NonPendingCannotAccept() public {
        address newOwner = makeAddr("newOwner");
        vm.prank(owner);
        escrow.transferOwnership(newOwner);
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.acceptOwnership();
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, owner));
        escrow.acceptOwnership();
        assertEq(escrow.owner(), owner);
    }

    function test_Ownership_OnlyOwnerCanStartTransfer() public {
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.transferOwnership(eve);
    }

    function test_Ownership_ZeroCancelsPending() public {
        vm.startPrank(owner);
        escrow.transferOwnership(eve);
        escrow.transferOwnership(address(0));
        vm.stopPrank();
        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.acceptOwnership();
    }

    function test_Ownership_RenounceDisabled() public {
        vm.prank(owner);
        vm.expectRevert(AgentRunEscrow.RenounceOwnershipDisabled.selector);
        escrow.renounceOwnership();
        assertEq(escrow.owner(), owner);

        vm.prank(eve);
        vm.expectRevert(abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, eve));
        escrow.renounceOwnership();
    }

    // ───────────────────────── nothing is payable ─────────────────────────

    function _rejectsValue(address from, bytes memory data) internal {
        vm.deal(from, 1 ether);
        vm.prank(from);
        (bool ok, bytes memory ret) = address(escrow).call{value: 1 wei}(data);
        assertFalse(ok, "value call must revert");
        assertEq(ret.length, 0, "must be the non-payable dispatcher revert (empty data)");
        assertEq(address(escrow).balance, 0);
    }

    function test_NoFunctionIsPayable() public {
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](1);
        cs[0] = _call(payee, 1e6, REF);
        _open();

        _rejectsValue(agent, abi.encodeCall(escrow.openRun, (runIdOf(agent, 2), BUDGET, runDeadline)));
        _rejectsValue(relayer, abi.encodeCall(escrow.openRunWithPermit2, (agent, runIdOf(agent, 3), BUDGET, runDeadline, 0, 0, "")));
        _rejectsValue(settler, abi.encodeCall(escrow.settleCall, (rid, payee, 1e6, REF)));
        _rejectsValue(settler, abi.encodeCall(escrow.settleCallBatch, (rid, cs)));
        _rejectsValue(agent, abi.encodeCall(escrow.closeRun, (rid)));
        _rejectsValue(owner, abi.encodeCall(escrow.setFeeBps, (uint16(5))));
        _rejectsValue(owner, abi.encodeCall(escrow.setFeeRecipient, (eve)));
        _rejectsValue(owner, abi.encodeCall(escrow.setSettler, (eve)));
        _rejectsValue(owner, abi.encodeCall(escrow.transferOwnership, (eve)));
        _rejectsValue(eve, abi.encodeCall(escrow.acceptOwnership, ()));
        _rejectsValue(owner, abi.encodeCall(escrow.renounceOwnership, ()));
        // plain ETH transfers and unknown selectors are rejected as well
        _rejectsValue(eve, "");
        _rejectsValue(eve, hex"deadbeef");
        // state untouched
        assertTrue(escrow.isRunOpen(rid));
        assertEq(_spent(rid), 0);
    }
}
