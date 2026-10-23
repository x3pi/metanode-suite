// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title ChildContract
 * @dev Hợp đồng con được sinh ra bởi Factory thông qua opcode CREATE.
 */
contract ChildContract {
    address public immutable creator;
    uint256 public immutable createdAtBlock;
    uint256 public immutable childId;

    event Initialized(address indexed creator, uint256 childId, uint256 blockNumber);

    constructor(uint256 _childId) {
        creator = msg.sender;
        createdAtBlock = block.number;
        childId = _childId;
        emit Initialized(msg.sender, _childId, block.number);
    }

    function getInfo() external view returns (address, uint256, uint256) {
        return (creator, childId, createdAtBlock);
    }
}

/**
 * @title Factory
 * @dev Smart contract làm mục tiêu ủy quyền cho EIP-7702.
 * Khi một ví EOA ủy quyền sang Factory này:
 * 1. Lệnh gọi createChild(id) sẽ thực thi opcode CREATE (new ChildContract).
 * 2. Lệnh gọi vào Fallback / Receive cũng kích hoạt opcode CREATE.
 * 3. Khi CREATE thực thi, tài khoản EOA ủy quyền sẽ được tăng nonce thêm 1.
 */
contract Factory {
    event ChildDeployed(address indexed childAddress, address indexed deployer, uint256 childId);

    address public lastChild;
    uint256 public totalChildren;

    /**
     * @notice Tạo hợp đồng con mới bằng opcode CREATE
     * @param _id Mã định danh cho hợp đồng con
     * @return childAddr Địa chỉ hợp đồng con vừa tạo
     */
    function createChild(uint256 _id) public returns (address childAddr) {
        ChildContract child = new ChildContract(_id);
        childAddr = address(child);
        lastChild = childAddr;
        totalChildren++;
        emit ChildDeployed(childAddr, msg.sender, _id);
    }

    /**
     * @notice Fallback kích hoạt khi ví ủy quyền nhận bất kỳ calldata nào
     */
    fallback() external payable {
        createChild(totalChildren + 1);
    }

    /**
     * @notice Receive kích hoạt khi ví ủy quyền nhận chuyển tiền native
     */
    receive() external payable {
        createChild(totalChildren + 1);
    }
}
