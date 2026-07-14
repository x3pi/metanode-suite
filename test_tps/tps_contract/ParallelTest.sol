// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ParallelTest {
    mapping(address => uint256) public values;

    function getValue(address user) public view returns (uint256) {
        return values[user];
    }

    function updateState(uint256 val) public {
        values[msg.sender] = val;
    }
}
