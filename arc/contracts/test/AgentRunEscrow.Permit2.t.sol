// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";
import {FaithfulPermit2, LaxPermit2} from "./mocks/Permit2Mocks.sol";

contract AgentRunEscrowPermit2Test is EscrowTestBase {
    bytes32 internal constant REF = bytes32("ref");

    string internal constant WITNESS_TYPE_STRING =
        "RunWitness witness)RunWitness(bytes32 runId,address agent,uint256 budget,uint64 deadline)TokenPermissions(address token,uint256 amount)";
    bytes32 internal constant RUN_WITNESS_TYPEHASH =
        keccak256("RunWitness(bytes32 runId,address agent,uint256 budget,uint64 deadline)");
    bytes32 internal constant PERMIT_TYPEHASH = keccak256(
        "PermitWitnessTransferFrom(TokenPermissions permitted,address spender,uint256 nonce,uint256 deadline,RunWitness witness)RunWitness(bytes32 runId,address agent,uint256 budget,uint64 deadline)TokenPermissions(address token,uint256 amount)"
    );
    bytes32 internal constant TOKEN_PERMISSIONS_TYPEHASH = keccak256("TokenPermissions(address token,uint256 amount)");

    struct P {
        bytes32 id;
        address who;
        uint256 budget;
        uint64 dl;
        uint256 nonce;
        uint256 permitDl;
    }

    address internal pAgent;
    uint256 internal pk;

    function setUp() public override {
        super.setUp();
        (pAgent, pk) = makeAddrAndKey("permitAgent");
        usdc.mint(pAgent, 100_000e6);
        vm.prank(pAgent);
        usdc.approve(address(permit2), type(uint256).max); // NOTE: no allowance to the escrow
    }

    // ───────────────────────── helpers ─────────────────────────

    function _p(uint96 tag, uint256 nonce) internal view returns (P memory p) {
        p = P({
            id: runIdOf(pAgent, tag),
            who: pAgent,
            budget: BUDGET,
            dl: runDeadline,
            nonce: nonce,
            permitDl: block.timestamp + 1 hours
        });
    }

    function _digest(P memory p, address spender) internal view returns (bytes32) {
        bytes32 witness = keccak256(abi.encode(RUN_WITNESS_TYPEHASH, p.id, p.who, p.budget, p.dl));
        bytes32 tokenPerms = keccak256(abi.encode(TOKEN_PERMISSIONS_TYPEHASH, address(usdc), p.budget));
        bytes32 structHash = keccak256(abi.encode(PERMIT_TYPEHASH, tokenPerms, spender, p.nonce, p.permitDl, witness));
        return keccak256(abi.encodePacked("\x19\x01", permit2.DOMAIN_SEPARATOR(), structHash));
    }

    function _sign(uint256 key, P memory p, address spender) internal view returns (bytes memory) {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, _digest(p, spender));
        return abi.encodePacked(r, s, v);
    }

    function _submit(AgentRunEscrow e, P memory p, bytes memory sig) internal {
        vm.prank(relayer);
        e.openRunWithPermit2(p.who, p.id, p.budget, p.dl, p.nonce, p.permitDl, sig);
    }

    // ───────────────────────── static wiring ─────────────────────────

    function test_Permit2_WitnessConstantsMatchSpec() public view {
        assertEq(escrow.WITNESS_TYPE_STRING(), WITNESS_TYPE_STRING);
        assertEq(escrow.RUN_WITNESS_TYPEHASH(), RUN_WITNESS_TYPEHASH);
    }

    // ───────────────────────── happy path (real EIP-712 verification) ─────────────────────────

    function test_Permit2_RelayerOpensForGaslessAgent() public {
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));
        assertEq(pAgent.balance, 0, "agent has no gas");
        assertEq(usdc.allowance(pAgent, address(escrow)), 0, "no direct allowance to the escrow");
        uint256 agentBefore = usdc.balanceOf(pAgent);
        uint256 relayerBefore = usdc.balanceOf(relayer);

        vm.expectEmit(true, true, false, true, address(escrow));
        emit RunOpened(p.id, pAgent, BUDGET, runDeadline);
        _submit(escrow, p, sig);

        assertEq(usdc.balanceOf(address(escrow)), BUDGET);
        assertEq(usdc.balanceOf(pAgent), agentBefore - BUDGET);
        assertEq(usdc.balanceOf(relayer), relayerBefore, "relayer pays nothing and gets nothing");
        (address a, uint256 b, uint256 s, uint64 d, bool o, uint16 f) = escrow.runs(p.id);
        assertEq(a, pAgent);
        assertEq(b, BUDGET);
        assertEq(s, 0);
        assertEq(d, runDeadline);
        assertTrue(o);
        assertEq(f, FEE_BPS);

        // full lifecycle: settle, then the agent (no gas needed in-test) closes and is refunded
        _settle(p.id, payee, 100e6);
        vm.prank(pAgent);
        escrow.closeRun(p.id);
        assertEq(usdc.balanceOf(pAgent), agentBefore - 101e6);
        assertEq(usdc.balanceOf(relayer), relayerBefore);
    }

    function test_Permit2_AgentItselfMayAlsoSubmit() public {
        P memory p = _p(1, 3);
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.prank(pAgent);
        escrow.openRunWithPermit2(p.who, p.id, p.budget, p.dl, p.nonce, p.permitDl, sig);
        assertTrue(escrow.isRunOpen(p.id));
    }

    function test_Permit2_PermitDeadlineIsIndependentOfRunDeadline() public {
        P memory p = _p(1, 0);
        p.permitDl = block.timestamp + 10; // permit expires long before run deadline
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.warp(p.permitDl); // exactly at permit deadline still valid
        _submit(escrow, p, sig);
        assertTrue(escrow.isRunOpen(p.id));

        P memory q = _p(2, 1);
        q.permitDl = block.timestamp + 10;
        bytes memory sig2 = _sign(pk, q, address(escrow));
        vm.warp(q.permitDl + 1);
        vm.expectRevert(abi.encodeWithSelector(FaithfulPermit2.SignatureExpired.selector, q.permitDl));
        _submit(escrow, q, sig2);
        assertFalse(escrow.isRunOpen(q.id));
    }

    // ───────────────────────── tampering ─────────────────────────

    function test_Permit2_TamperedFieldsFail() public {
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));

        P memory t = _p(1, 0);
        t.budget = BUDGET - 1;
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        t = _p(1, 0);
        t.budget = BUDGET + 1;
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        t = _p(1, 0);
        t.dl = runDeadline + 1;
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        t = _p(1, 0);
        t.id = runIdOf(pAgent, 2); // different tag within the agent's own namespace
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        t = _p(1, 0);
        t.nonce = 1;
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        t = _p(1, 0);
        t.permitDl += 1;
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, t, sig);

        assertFalse(escrow.isRunOpen(p.id));
        assertEq(usdc.balanceOf(address(escrow)), 0);

        // the untampered payload still works afterwards
        _submit(escrow, p, sig);
        assertTrue(escrow.isRunOpen(p.id));
    }

    function test_Permit2_WrongSignerAndSpoofedAgentFail() public {
        (, uint256 eveKey) = makeAddrAndKey("eveKey");
        P memory p = _p(1, 0);
        // signed by someone else
        bytes memory badSig = _sign(eveKey, p, address(escrow));
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, p, badSig);

        // eve signs for victim's namespace claiming victim as the owner
        bytes memory garbage = hex"1234";
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, p, garbage);
    }

    function test_Permit2_SignatureBoundToThisEscrowAsSpender() public {
        AgentRunEscrow other = _newEscrow(address(usdc), address(permit2), FEE_BPS);
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(other, p, sig); // same signature replayed at another escrow
        assertFalse(other.isRunOpen(p.id));
        _submit(escrow, p, sig);
    }

    function test_Permit2_RunIdMustMatchAgentEvenWithValidSignature() public {
        // agent signs a witness binding a runId that is NOT in its own namespace
        P memory p = _p(1, 0);
        p.id = runIdOf(eve, 1);
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunIdAgentMismatch.selector, p.id, pAgent));
        _submit(escrow, p, sig);
    }

    function test_Permit2_RelayerCannotSquatWithOwnAgentClaim() public {
        // relayer claims the victim's runId for itself via the permit path: needs the victim's signature
        P memory p = _p(1, 0);
        p.who = relayer;
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunIdAgentMismatch.selector, p.id, relayer));
        _submit(escrow, p, sig);

        // relayer uses its own matching id but victim's key signature: owner (relayer) never signed
        p.id = runIdOf(relayer, 1);
        vm.expectRevert(FaithfulPermit2.InvalidSigner.selector);
        _submit(escrow, p, sig);
    }

    // ───────────────────────── replay ─────────────────────────

    function test_Permit2_ReplayedNonceFails() public {
        P memory p1 = _p(1, 7);
        _submit(escrow, p1, _sign(pk, p1, address(escrow)));
        assertTrue(permit2.nonceBitmap(pAgent, 0) & (1 << 7) != 0);

        // identical replay: escrow rejects the already-existing run before spending anything
        bytes memory sig1 = _sign(pk, p1, address(escrow));
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunAlreadyExists.selector, p1.id));
        _submit(escrow, p1, sig1);

        // a NEW run signed with the same nonce is rejected by Permit2
        P memory p2 = _p(2, 7);
        bytes memory sig2 = _sign(pk, p2, address(escrow));
        vm.expectRevert(FaithfulPermit2.InvalidNonce.selector);
        _submit(escrow, p2, sig2);
        assertFalse(escrow.isRunOpen(p2.id));

        // a fresh nonce works (unordered nonces)
        P memory p3 = _p(2, 300); // different bitmap word
        _submit(escrow, p3, _sign(pk, p3, address(escrow)));
        assertTrue(escrow.isRunOpen(p3.id));
        assertEq(usdc.balanceOf(address(escrow)), 2 * BUDGET);
    }

    function test_Permit2_ClosedRunCannotBeReopenedWithFreshSignature() public {
        P memory p1 = _p(1, 0);
        _submit(escrow, p1, _sign(pk, p1, address(escrow)));
        vm.prank(pAgent);
        escrow.closeRun(p1.id);

        P memory p2 = _p(1, 1);
        bytes memory sig = _sign(pk, p2, address(escrow));
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunAlreadyExists.selector, p1.id));
        _submit(escrow, p2, sig);
    }

    function test_Permit2_ExistingDirectRunIdAlsoBlocked() public {
        // a run opened directly by the agent occupies the id for the permit path too
        vm.startPrank(pAgent);
        usdc.approve(address(escrow), type(uint256).max);
        escrow.openRun(runIdOf(pAgent, 1), BUDGET, runDeadline);
        vm.stopPrank();
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.RunAlreadyExists.selector, p.id));
        _submit(escrow, p, sig);
    }

    // ───────────────────────── validation before funding ─────────────────────────

    function test_Permit2_ParameterValidation() public {
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));

        p.budget = 0;
        vm.expectRevert(AgentRunEscrow.InvalidBudget.selector);
        _submit(escrow, p, sig);

        p = _p(1, 0);
        p.dl = uint64(block.timestamp);
        vm.expectRevert(AgentRunEscrow.InvalidDeadline.selector);
        _submit(escrow, p, sig);

        p = _p(1, 0);
        p.who = address(0);
        p.id = bytes32(0);
        vm.expectRevert(AgentRunEscrow.ZeroAddress.selector);
        _submit(escrow, p, sig);
    }

    function test_Permit2_RevertsWithoutPermit2Allowance() public {
        vm.prank(pAgent);
        usdc.approve(address(permit2), 0);
        P memory p = _p(1, 0);
        bytes memory sig = _sign(pk, p, address(escrow));
        vm.expectRevert(); // ERC20InsufficientAllowance(permit2, ...)
        _submit(escrow, p, sig);
        assertFalse(escrow.isRunOpen(p.id));
    }

    // ───────────────────────── lax permit2: escrow-side wiring ─────────────────────────

    function _laxSetup() internal returns (LaxPermit2 lax, AgentRunEscrow e) {
        lax = new LaxPermit2();
        e = _newEscrow(address(usdc), address(lax), FEE_BPS);
        vm.prank(pAgent);
        usdc.approve(address(lax), type(uint256).max);
    }

    function test_Permit2_PassesCorrectArgumentsToPermit2() public {
        (LaxPermit2 lax, AgentRunEscrow e) = _laxSetup();
        P memory p = _p(1, 9);
        bytes memory sig = hex"aabbccdd";
        _submit(e, p, sig);

        assertEq(lax.calls(), 1);
        assertEq(lax.lastCaller(), address(e));
        assertEq(lax.lastToken(), address(usdc));
        assertEq(lax.lastPermittedAmount(), BUDGET);
        assertEq(lax.lastNonce(), 9);
        assertEq(lax.lastPermitDeadline(), p.permitDl);
        assertEq(lax.lastTo(), address(e));
        assertEq(lax.lastRequested(), BUDGET);
        assertEq(lax.lastOwner(), pAgent);
        assertEq(lax.lastWitness(), keccak256(abi.encode(RUN_WITNESS_TYPEHASH, p.id, pAgent, BUDGET, runDeadline)));
        assertEq(lax.lastWitnessTypeString(), WITNESS_TYPE_STRING);
        assertEq(lax.lastSignature(), sig);
        assertTrue(e.isRunOpen(p.id));
    }

    function test_Permit2_UnderDeliveryRevertsFundingMismatch() public {
        (LaxPermit2 lax, AgentRunEscrow e) = _laxSetup();
        P memory p = _p(1, 0);

        lax.setShortfall(1);
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.FundingAmountMismatch.selector, BUDGET, BUDGET - 1));
        _submit(e, p, "");

        lax.setShortfall(BUDGET); // permit2 does nothing at all
        vm.expectRevert(abi.encodeWithSelector(AgentRunEscrow.FundingAmountMismatch.selector, BUDGET, 0));
        _submit(e, p, "");

        assertFalse(e.isRunOpen(p.id), "run not recorded on failed funding");
        assertEq(usdc.balanceOf(address(e)), 0);
    }
}
