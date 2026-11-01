// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

interface IERC20 {
    function transferFrom(address from, address to, uint256 value) external returns (bool);
    function transfer(address to, uint256 value) external returns (bool);
}

/// @title AgentMeshEscrow
/// @notice Holds ERC-20 USDC (6 decimals) for one deal id. Status is written
///         before the token call. This contract never reads address.balance:
///         on Arc that figure is 18-decimal native USDC and is a different
///         unit from the ERC-20 amount stored here.
contract AgentMeshEscrow {
    struct Deal {
        address payer;
        address payee;
        address token;
        uint256 amount;
        uint8 status;
    }

    mapping(bytes32 => Deal) public deals;

    error ZeroAddress();
    error BadAmount();
    error BadStatus();
    error TransferFailed();

    function fund(bytes32 id, address token, address payee, uint256 amount) external {
        if (token == address(0) || payee == address(0)) revert ZeroAddress();
        if (amount == 0) revert BadAmount();
        Deal storage deal = deals[id];
        if (deal.status != 0) revert BadStatus();
        deal.payer = msg.sender;
        deal.payee = payee;
        deal.token = token;
        deal.amount = amount;
        deal.status = 1;
        if (!IERC20(token).transferFrom(msg.sender, address(this), amount)) revert TransferFailed();
    }

    function release(bytes32 id) external {
        Deal storage deal = deals[id];
        if (deal.status != 1) revert BadStatus();
        if (msg.sender != deal.payer && msg.sender != deal.payee) revert BadStatus();
        address payee = deal.payee;
        address token = deal.token;
        uint256 amount = deal.amount;
        deal.status = 2;
        if (!IERC20(token).transfer(payee, amount)) revert TransferFailed();
    }

    function refund(bytes32 id) external {
        Deal storage deal = deals[id];
        if (deal.status != 1) revert BadStatus();
        if (msg.sender != deal.payer && msg.sender != deal.payee) revert BadStatus();
        address payer = deal.payer;
        address token = deal.token;
        uint256 amount = deal.amount;
        deal.status = 3;
        if (!IERC20(token).transfer(payer, amount)) revert TransferFailed();
    }
}
