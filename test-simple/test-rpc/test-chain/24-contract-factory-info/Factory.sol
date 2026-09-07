// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Child {
    uint public x;
    constructor(uint _x) {
        x = _x;
    }
}

contract Factory {
    event ChildCreated(address childAddress);
    
    function createChild(uint _x) public returns (address) {
        Child c = new Child(_x);
        emit ChildCreated(address(c));
        return address(c);
    }
}
