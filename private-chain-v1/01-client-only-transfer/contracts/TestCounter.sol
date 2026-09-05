// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title TestCounter
 * @notice Smart Contract mẫu triển khai trên Chain Đích trong kịch bản Cross-Chain.
 * @dev Nhận cuộc gọi hàm `increment()` từ Chain Nguồn thông qua Relayer & Gateway.
 */
contract TestCounter {
    uint256 private count;

    // Sự kiện phát ra mỗi khi biến đếm được tăng
    event Incremented(uint256 indexed newCount, address indexed caller);

    /**
     * @notice Tăng giá trị biến đếm lên 1
     * @dev Function selector của hàm này là: 0xd09de08a (keccak256("increment()")[:4])
     */
    function increment() external {
        count += 1;
        emit Incremented(count, msg.sender);
    }

    /**
     * @notice Đọc giá trị hiện tại của biến đếm
     * @return Giá trị count hiện thời
     */
    function getCount() external view returns (uint256) {
        return count;
    }
}
