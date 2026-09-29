// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @notice Probe used by the RPC regression test. Both functions are views so
/// they execute through eth_call / eth_estimateGas simulation paths.
contract BlockTimestampRPCSimulationTest {
    function currentTimestamp() external view returns (uint256) {
        return block.timestamp;
    }

    function isBefore(uint256 deadline) external view returns (bool) {
        return block.timestamp < deadline;
    }

    function requireBefore(uint256 deadline) external view {
        require(block.timestamp < deadline, "expired");
    }
}
