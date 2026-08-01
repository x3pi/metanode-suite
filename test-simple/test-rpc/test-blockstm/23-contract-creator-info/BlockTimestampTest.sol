// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/**
 * @title BlockContextTest
 * @dev Bài test kiểm tra toàn bộ các giá trị của BlockContext từ EVM.
 */
contract BlockContextTest {
    uint256 public savedTimestamp;
    uint256 public savedNumber;
    address public savedCoinbase;
    uint256 public savedChainId;
    uint256 public savedBaseFee;

    function saveAll() external {
        savedTimestamp = block.timestamp;
        savedNumber = block.number;
        savedCoinbase = block.coinbase;
        savedChainId = block.chainid;
        savedBaseFee = block.basefee;
    }
}
