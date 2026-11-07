// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/// @dev 6-decimal USDC mock with mint helper and a blocklist (reverts like Circle USDC).
contract MockUSDC is ERC20 {
    mapping(address => bool) public blocked;

    error Blocked(address account);

    constructor() ERC20("Mock USDC", "mUSDC") {}

    function decimals() public pure override returns (uint8) {
        return 6;
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function setBlocked(address account, bool isBlocked) external {
        blocked[account] = isBlocked;
    }

    function _update(address from, address to, uint256 value) internal virtual override {
        if (blocked[from]) revert Blocked(from);
        if (blocked[to]) revert Blocked(to);
        super._update(from, to, value);
    }
}

/// @dev Non-reverting token: transfer/transferFrom return false (no state change) for flagged recipients.
contract FalseReturnUSDC is MockUSDC {
    mapping(address => bool) public falseFor;

    function setFalseFor(address account, bool flag) external {
        falseFor[account] = flag;
    }

    function transfer(address to, uint256 value) public override returns (bool) {
        if (falseFor[to]) return false;
        return super.transfer(to, value);
    }

    function transferFrom(address from, address to, uint256 value) public override returns (bool) {
        if (falseFor[to]) return false;
        return super.transferFrom(from, to, value);
    }
}

/// @dev Burns 1% of every non-mint/non-burn transfer.
contract FeeOnTransferUSDC is MockUSDC {
    bool public feeOn = true;

    function setFeeOn(bool on) external {
        feeOn = on;
    }

    function _update(address from, address to, uint256 value) internal override {
        if (feeOn && from != address(0) && to != address(0)) {
            uint256 cut = value / 100;
            super._update(from, to, value - cut);
            super._update(from, address(0), cut);
        } else {
            super._update(from, to, value);
        }
    }
}

/// @dev ERC-777-style hook: after a transfer (not a mint) to `hookWhen`, calls `hookTarget.onTokenHook()`.
contract HookUSDC is MockUSDC {
    address public hookWhen;
    address public hookTarget;

    function setHook(address when_, address target_) external {
        hookWhen = when_;
        hookTarget = target_;
    }

    function _update(address from, address to, uint256 value) internal override {
        super._update(from, to, value);
        if (from != address(0) && hookWhen != address(0) && to == hookWhen && hookTarget != address(0)) {
            (bool ok, bytes memory ret) = hookTarget.call(abi.encodeWithSignature("onTokenHook()"));
            if (!ok) {
                assembly {
                    revert(add(ret, 32), mload(ret))
                }
            }
        }
    }
}

/// @dev Acts as agent/settler/payee; on hook it replays `attackCall` against `target`.
contract ReentrantActor {
    address public target;
    bytes public attackCall;
    bool public bubble;
    bool public lastOk;
    bytes public lastRet;
    uint256 public hits;

    function configure(address target_, bytes calldata attackCall_, bool bubble_) external {
        target = target_;
        attackCall = attackCall_;
        bubble = bubble_;
    }

    /// @dev Execute an arbitrary call as this contract (open runs, approve, etc.).
    function exec(address to, bytes calldata data) external returns (bytes memory) {
        (bool ok, bytes memory ret) = to.call(data);
        if (!ok) {
            assembly {
                revert(add(ret, 32), mload(ret))
            }
        }
        return ret;
    }

    function onTokenHook() external {
        hits++;
        (bool ok, bytes memory ret) = target.call(attackCall);
        lastOk = ok;
        lastRet = ret;
        if (bubble && !ok) {
            assembly {
                revert(add(ret, 32), mload(ret))
            }
        }
    }
}
