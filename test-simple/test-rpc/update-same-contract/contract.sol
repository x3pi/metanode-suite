// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @title TestCounter - Sequential increment stress test contract
contract TestCounter {
    uint256 private count;

    // Emitted on each successful increment
    event Incremented(uint256 newCount);

    /// @notice Increment the counter by 1 and emit the new value
    function increment() external {
        count += 1;
        emit Incremented(count);
    }

    /// @notice Read the current counter value (view, no gas)
    function getCount() external view returns (uint256) {
        return count;
    }
}
