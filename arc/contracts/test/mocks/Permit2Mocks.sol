// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import {IPermit2} from "../../AgentRunEscrow.sol";

/// @dev Faithful minimal re-implementation of Permit2 SignatureTransfer.permitWitnessTransferFrom
/// (EIP-712 domain "Permit2", unordered-nonce bitmap, expiry, amount cap, signer check).
contract FaithfulPermit2 is IPermit2 {
    using SafeERC20 for IERC20;

    error InvalidSigner();
    error InvalidNonce();
    error SignatureExpired(uint256 signatureDeadline);
    error InvalidAmount(uint256 maxAmount);

    bytes32 public constant TOKEN_PERMISSIONS_TYPEHASH = keccak256("TokenPermissions(address token,uint256 amount)");
    string public constant WITNESS_STUB =
        "PermitWitnessTransferFrom(TokenPermissions permitted,address spender,uint256 nonce,uint256 deadline,";

    mapping(address => mapping(uint256 => uint256)) public nonceBitmap;

    function DOMAIN_SEPARATOR() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,uint256 chainId,address verifyingContract)"),
                keccak256("Permit2"),
                block.chainid,
                address(this)
            )
        );
    }

    function permitWitnessTransferFrom(
        PermitTransferFrom calldata permit,
        SignatureTransferDetails calldata transferDetails,
        address owner,
        bytes32 witness,
        string calldata witnessTypeString,
        bytes calldata signature
    ) external {
        if (block.timestamp > permit.deadline) revert SignatureExpired(permit.deadline);
        if (transferDetails.requestedAmount > permit.permitted.amount) {
            revert InvalidAmount(permit.permitted.amount);
        }

        uint256 wordPos = permit.nonce >> 8;
        uint256 bit = 1 << uint8(permit.nonce);
        uint256 word = nonceBitmap[owner][wordPos];
        if (word & bit != 0) revert InvalidNonce();
        nonceBitmap[owner][wordPos] = word | bit;

        (address signer,,) = ECDSA.tryRecover(_digest(permit, witness, witnessTypeString), signature);
        if (signer == address(0) || signer != owner) revert InvalidSigner();

        IERC20(permit.permitted.token).safeTransferFrom(owner, transferDetails.to, transferDetails.requestedAmount);
    }

    function _digest(PermitTransferFrom calldata permit, bytes32 witness, string calldata witnessTypeString)
        private
        view
        returns (bytes32)
    {
        bytes32 typeHash = keccak256(abi.encodePacked(WITNESS_STUB, witnessTypeString));
        bytes32 tokenPermissionsHash = keccak256(abi.encode(TOKEN_PERMISSIONS_TYPEHASH, permit.permitted));
        bytes32 dataHash =
            keccak256(abi.encode(typeHash, tokenPermissionsHash, msg.sender, permit.nonce, permit.deadline, witness));
        return keccak256(abi.encodePacked("\x19\x01", DOMAIN_SEPARATOR(), dataHash));
    }
}

/// @dev Verifies NOTHING. Records the arguments it received and optionally under-delivers.
contract LaxPermit2 is IPermit2 {
    using SafeERC20 for IERC20;

    uint256 public shortfall;
    uint256 public calls;
    address public lastCaller;
    address public lastToken;
    uint256 public lastPermittedAmount;
    uint256 public lastNonce;
    uint256 public lastPermitDeadline;
    address public lastTo;
    uint256 public lastRequested;
    address public lastOwner;
    bytes32 public lastWitness;
    string public lastWitnessTypeString;
    bytes public lastSignature;

    function setShortfall(uint256 s) external {
        shortfall = s;
    }

    function permitWitnessTransferFrom(
        PermitTransferFrom calldata permit,
        SignatureTransferDetails calldata transferDetails,
        address owner,
        bytes32 witness,
        string calldata witnessTypeString,
        bytes calldata signature
    ) external {
        calls++;
        lastCaller = msg.sender;
        lastToken = permit.permitted.token;
        lastPermittedAmount = permit.permitted.amount;
        lastNonce = permit.nonce;
        lastPermitDeadline = permit.deadline;
        lastTo = transferDetails.to;
        lastRequested = transferDetails.requestedAmount;
        lastOwner = owner;
        lastWitness = witness;
        lastWitnessTypeString = witnessTypeString;
        lastSignature = signature;
        uint256 amt = transferDetails.requestedAmount - shortfall;
        if (amt > 0) IERC20(permit.permitted.token).safeTransferFrom(owner, transferDetails.to, amt);
    }
}
