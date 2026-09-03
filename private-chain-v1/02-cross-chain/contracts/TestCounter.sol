// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title TestCounter
 * @dev Smart Contract đích trên Chain B, nhận lệnh gọi hàm `increment()` từ Chain A
 *      thông qua hệ thống Metanode Cross-Chain Gateway Precompile (0x1002).
 */
contract TestCounter {
    // Biến đếm lưu số lần hàm increment() được gọi
    uint256 private count;

    // Địa chỉ của người/contract gọi hàm gần nhất
    address public lastCaller;

    // Timestamp của lần gọi gần nhất
    uint256 public lastCalledAt;

    // Sự kiện phát ra khi bộ đếm được tăng
    event Incremented(uint256 newCount, address indexed caller, uint256 timestamp);

    /**
     * @notice Tăng bộ đếm lên 1 đơn vị và ghi nhận thông tin caller
     * @dev Function selector: 0xd09de08a (`increment()`)
     */
    function increment() external {
        count += 1;
        lastCaller = msg.sender;
        lastCalledAt = block.timestamp;

        emit Incremented(count, msg.sender, block.timestamp);
    }

    /**
     * @notice Đọc giá trị hiện tại của bộ đếm (View, không tốn gas)
     * @dev Function selector: 0xa87d942c (`getCount()`)
     * @return Giá trị hiện tại của biến đếm
     */
    function getCount() external view returns (uint256) {
        return count;
    }

    /**
     * @notice Đặt lại bộ đếm về 0 (tùy chọn để tái sử dụng kiểm thử)
     */
    function reset() external {
        count = 0;
    }
}
