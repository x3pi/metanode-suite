// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract BalanceChecker {
    uint256 public bal1;
    uint256 public bal3;
    uint256 public contractBalance;
    
    // Biến count (đã có public nên tự động có hàm xem giá trị)
    uint256 public count; 

    function step1() public {
        bal1 = msg.sender.balance;
    }

    function step3() public {
        bal3 = msg.sender.balance;
    }

    function deposit() public payable {
        bal1 = msg.sender.balance; // Capture balance
        contractBalance = address(this).balance;
    }

    receive() external payable {
        contractBalance = address(this).balance;
    }
}