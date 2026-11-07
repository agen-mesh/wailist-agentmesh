// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {AgentRunEscrow} from "../AgentRunEscrow.sol";
import {MockUSDC} from "./mocks/Tokens.sol";
import {EscrowTestBase} from "./AgentRunEscrow.Base.t.sol";

/// @dev Drives bounded, mostly-valid actions against the escrow.
contract EscrowHandler is Test {
    AgentRunEscrow public immutable escrow;
    MockUSDC public immutable usdc;
    address public immutable owner;
    address public immutable settler;

    address[3] public agents;
    address[3] public payees;
    bytes32[] public runIds;
    uint96 public tagCounter;
    uint256 public okSettles;

    constructor(AgentRunEscrow escrow_, MockUSDC usdc_, address owner_, address settler_) {
        escrow = escrow_;
        usdc = usdc_;
        owner = owner_;
        settler = settler_;
        for (uint256 i; i < 3; ++i) {
            agents[i] = makeAddr(string.concat("hAgent", vm.toString(i)));
            payees[i] = makeAddr(string.concat("hPayee", vm.toString(i)));
            usdc.mint(agents[i], 1e18);
            vm.prank(agents[i]);
            usdc.approve(address(escrow), type(uint256).max);
        }
    }

    function runCount() external view returns (uint256) {
        return runIds.length;
    }

    function open(uint256 agentSeed, uint256 budgetSeed, uint256 ttlSeed) external {
        address a = agents[agentSeed % 3];
        uint256 budget = bound(budgetSeed, 1, 1e12);
        uint64 dl = uint64(block.timestamp + bound(ttlSeed, 1, 10 days));
        bytes32 id = bytes32(abi.encodePacked(a, bytes12(++tagCounter)));
        vm.prank(a);
        escrow.openRun(id, budget, dl);
        runIds.push(id);
    }

    function settle(uint256 runSeed, uint256 payeeSeed, uint256 amountSeed) external {
        if (runIds.length == 0) return;
        bytes32 id = runIds[runSeed % runIds.length];
        uint256 amount = bound(amountSeed, 1, 5e11);
        vm.prank(settler);
        try escrow.settleCall(id, payees[payeeSeed % 3], amount, bytes32(amountSeed)) {
            okSettles++;
        } catch {}
    }

    function batch(uint256 runSeed, uint256 payeeSeed, uint256 amountSeed, uint256 nSeed) external {
        if (runIds.length == 0) return;
        bytes32 id = runIds[runSeed % runIds.length];
        uint256 n = bound(nSeed, 1, 8);
        AgentRunEscrow.Call[] memory cs = new AgentRunEscrow.Call[](n);
        for (uint256 i; i < n; ++i) {
            uint256 h = uint256(keccak256(abi.encode(amountSeed, i)));
            cs[i] = AgentRunEscrow.Call({payee: payees[(payeeSeed + i) % 3], amount: bound(h, 1, 1e11), toolRef: bytes32(h)});
        }
        vm.prank(settler);
        try escrow.settleCallBatch(id, cs) {} catch {}
    }

    function close(uint256 runSeed, bool bySettler) external {
        if (runIds.length == 0) return;
        bytes32 id = runIds[runSeed % runIds.length];
        (address a,,,,,) = escrow.runs(id);
        vm.prank(bySettler ? settler : a);
        try escrow.closeRun(id) {} catch {}
    }

    function setFee(uint256 feeSeed) external {
        vm.prank(owner);
        escrow.setFeeBps(uint16(bound(feeSeed, 0, 1000)));
    }

    function warp(uint256 secs) external {
        vm.warp(block.timestamp + bound(secs, 0, 2 days));
    }
}

contract AgentRunEscrowInvariantTest is EscrowTestBase {
    EscrowHandler internal handler;

    function setUp() public override {
        super.setUp();
        handler = new EscrowHandler(escrow, usdc, owner, settler);
        targetContract(address(handler));
        bytes4[] memory sels = new bytes4[](5);
        sels[0] = EscrowHandler.open.selector;
        sels[1] = EscrowHandler.settle.selector;
        sels[2] = EscrowHandler.batch.selector;
        sels[3] = EscrowHandler.close.selector;
        sels[4] = EscrowHandler.warp.selector;
        targetSelector(FuzzSelector({addr: address(handler), selectors: sels}));
        bytes4[] memory more = new bytes4[](1);
        more[0] = EscrowHandler.setFee.selector;
        targetSelector(FuzzSelector({addr: address(handler), selectors: more}));
    }

    /// non-vacuity probe: across all invariant runs some settles must have succeeded
    function afterInvariant() public view {
        assertGt(handler.okSettles(), 0, "handler never produced a successful settle");
    }

    /// escrow USDC balance == sum over OPEN runs of (budget - spent)
    function invariant_BalanceEqualsSumOfOpenRemaining() public view {
        uint256 sum;
        uint256 n = handler.runCount();
        for (uint256 i; i < n; ++i) {
            (, uint256 budget, uint256 spent,, bool open_,) = escrow.runs(handler.runIds(i));
            if (open_) sum += budget - spent;
        }
        assertEq(usdc.balanceOf(address(escrow)), sum);
    }

    /// spent <= budget for every run, open or closed
    function invariant_SpentNeverExceedsBudget() public view {
        uint256 n = handler.runCount();
        for (uint256 i; i < n; ++i) {
            (, uint256 budget, uint256 spent,,,) = escrow.runs(handler.runIds(i));
            assertLe(spent, budget);
        }
    }

    /// everything ever spent has been paid to payees + fee recipient; nothing is created or lost
    function invariant_SpentEqualsPaidOutAndSupplyConserved() public view {
        uint256 spentSum;
        uint256 n = handler.runCount();
        for (uint256 i; i < n; ++i) {
            (,, uint256 spent,,,) = escrow.runs(handler.runIds(i));
            spentSum += spent;
        }
        uint256 paid = usdc.balanceOf(feeRecipient);
        uint256 held = usdc.balanceOf(address(escrow));
        for (uint256 i; i < 3; ++i) {
            paid += usdc.balanceOf(handler.payees(i));
            held += usdc.balanceOf(handler.agents(i));
        }
        assertEq(paid, spentSum, "paid out == sum(spent)");
        assertEq(paid + held, usdc.totalSupply() - usdc.balanceOf(agent) - usdc.balanceOf(address(0)), "conservation");
    }
}
