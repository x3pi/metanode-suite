// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title IGateway
 * @dev Interface của Metanode Cross-Chain Gateway Precompile tại địa chỉ:
 *      0x0000000000000000000000000000000000001002
 */
interface IGateway {
    /**
     * @notice Phát lệnh gửi tài sản hoặc dữ liệu gọi hàm xuyên chuỗi
     * @param destChainId ID của chain đích (hoặc Reserve Hub 991)
     * @param target Địa chỉ ví hoặc Smart Contract nhận trên chain đích
     * @param payload Dữ liệu calldata (đối với contract call) hoặc prefix routing
     * @param assetId Loại tài sản (0 = Native MTN Token)
     * @param value Số lượng token chuyển xuyên chuỗi (wei)
     * @param tip Tiền thưởng tip cho Relayer daemon xử lý gói tin
     * @param gasFee Phí trả trước cho node tại chain đích để thực thi EVM (đối với contract call)
     * @param hopCount Số chặng trung gian tối đa cho phép
     * @param ordered Có bắt buộc thực thi tuần tự theo nonce hay không
     * @return messageId Mã định danh duy nhất (bytes32) của message outbound
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
