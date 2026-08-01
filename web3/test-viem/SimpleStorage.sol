// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SimpleStorage {
    uint256 public value;
    
    event ValueChanged(uint256 newValue);

    function setValue(uint256 _value) public {
        value = _value;
        emit ValueChanged(_value);
    }

    function setValueRevert(uint256 _value) public {
        require(_value < 100, "Value too large, reverting!");
        value = _value;
        emit ValueChanged(_value);
    }
}
