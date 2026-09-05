// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title IGateway
 * @notice Interface của Metanode Cross-Chain Gateway Precompile tại địa chỉ:
 *         0x0000000000000000000000000000000000001002
 * @dev Dùng để phát lệnh gửi tài sản (native MTN) hoặc gọi hàm Smart Contract xuyên chuỗi.
 */
interface IGateway {
    /**
     * @notice Phát lệnh gửi tài sản hoặc dữ liệu gọi hàm xuyên chuỗi
     * @param destChainId ID của chain đích (ví dụ: 101, 102, hoặc Reserve Hub 991)
     * @param target      Địa chỉ ví nhận hoặc địa chỉ Smart Contract đích trên chain đích
     * @param payload     Calldata gọi hàm (đối với contract call) hoặc prefix rỗng (đối với transfer)
     * @param assetId     Loại tài sản (0 = Native MTN Token)
     * @param value       Số lượng token chuyển xuyên chuỗi (đơn vị: wei)
     * @param tip         Tiền thưởng tip cho Relayer daemon xử lý gói tin (wei)
     * @param gasFee      Phí trả trước cho node tại chain đích để thực thi EVM (wei)
     * @param hopCount    Số chặng trung gian tối đa cho phép (thường đặt = 1)
     * @param ordered     Bắt buộc thực thi tuần tự theo nonce (false = thực thi độc lập song song)
     * @return messageId  Mã định danh duy nhất (bytes32) của message outbound
     */
    function outbound(
        uint256 destChainId,
        address target,
        bytes calldata payload,
        uint256 assetId,
        uint256 value,
        uint256 tip,
        uint256 gasFee,
        uint8 hopCount,
        bool ordered
    ) external payable returns (bytes32 messageId);
}
