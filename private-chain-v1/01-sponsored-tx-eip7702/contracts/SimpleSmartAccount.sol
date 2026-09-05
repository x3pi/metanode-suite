// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title SimpleSmartAccount (EIP-7702 Delegation Target)
 * @notice Hợp đồng thông minh mẫu minh họa cho cơ chế Account Abstraction EIP-7702.
 * @dev Khi User (EOA) ký EIP-7702 Authorization tới địa chỉ hợp đồng này:
 *      - Mã bytecode của EOA trở thành designator: `0xef0100 || <address>`
 *      - Mọi cuộc gọi đến EOA sẽ thực thi mã của contract này trong ngữ cảnh (context) của chính EOA đó.
 *      - Ví Sponsor (Paymaster) có thể gửi SetCodeTx (TxType 0x04) và thanh toán 100% phí gas thay cho User.
 */
contract SimpleSmartAccount {
    // Sự kiện ghi lại các giao dịch được thực thi qua Smart Account
    event Executed(address indexed target, uint256 value, bytes data);
    event BatchExecuted(uint256 operationsCount);

    /**
     * @notice Thực thi đơn lẻ một giao dịch gọi hàm hoặc chuyển tiền từ Smart Account
     * @param target Địa chỉ hợp đồng hoặc ví nhận
     * @param value  Số lượng native token gửi kèm (wei)
     * @param data   Calldata thực thi
     * @return result Dữ liệu trả về từ cuộc gọi hàm
     */
    function execute(
        address target,
        uint256 value,
        bytes calldata data
    ) external payable returns (bytes memory result) {
        bool success;
        (success, result) = target.call{value: value}(data);
        require(success, "SimpleSmartAccount: execution failed");
        emit Executed(target, value, data);
    }

    /**
     * @notice Thực thi nhiều giao dịch gộp (Batch Execution) trong 1 atomic transaction duy nhất
     * @param targets Danh sách các địa chỉ đích
     * @param values  Danh sách số lượng native token gửi kèm
     * @param datas   Danh sách calldata thực thi tương ứng
     */
    function executeBatch(
        address[] calldata targets,
        uint256[] calldata values,
        bytes[] calldata datas
    ) external payable {
        require(
            targets.length == values.length && values.length == datas.length,
            "SimpleSmartAccount: array lengths mismatch"
        );

        for (uint256 i = 0; i < targets.length; i++) {
            (bool success, ) = targets[i].call{value: values[i]}(datas[i]);
            require(success, "SimpleSmartAccount: batch operation failed");
        }

        emit BatchExecuted(targets.length);
    }

    /// @notice Cho phép tài khoản nhận native token (MTN)
    receive() external payable {}
}
