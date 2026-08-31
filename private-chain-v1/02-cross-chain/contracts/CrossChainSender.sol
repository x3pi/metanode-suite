// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./IGateway.sol";

/**
 * @title CrossChainSender
 * @dev Ví dụ Smart Contract tại Chain nguồn (Chain A), minh họa cách gọi
 *      Gateway Precompile (0x1002) trực tiếp từ Solidity để gửi lệnh sang Chain khác.
 */
contract CrossChainSender {
    // Địa chỉ Gateway Precompile trên mọi node Metanode
    address public constant GATEWAY_ADDRESS = 0x0000000000000000000000000000000000001002;

    event CrossChainCallSent(bytes32 indexed messageId, uint256 destChainId, address target);
    event CrossChainTransferSent(bytes32 indexed messageId, uint256 destChainId, address recipient, uint256 amount);

    /**
     * @notice Gửi lệnh gọi hàm sang Smart Contract ở Chain khác
     * @param destChainId ID của chain đích (ví dụ 102)
     * @param targetContract Địa chỉ Smart Contract đích (ví dụ TestCounter trên Chain B)
     * @param payload Calldata của hàm cần gọi (ví dụ abi.encodeWithSignature("increment()"))
     * @param tip Phí tip cho Relayer
     * @param gasFee Phí execution gas cho remote EVM
     */
    function sendCrossChainCall(
        uint256 destChainId,
        address targetContract,
        bytes calldata payload,
        uint256 tip,
        uint256 gasFee
    ) external payable returns (bytes32 messageId) {
        uint256 totalRequired = tip + gasFee;
        require(msg.value >= totalRequired, "Insufficient ETH sent for tip and gasFee");

        messageId = IGateway(GATEWAY_ADDRESS).outbound{value: totalRequired}(
            destChainId,
            targetContract,
            payload,
            0,          // assetId = 0 (Native MTN)
            0,          // value = 0 (Chỉ gọi hàm, không chuyển token)
            tip,
            gasFee,
            1,          // hopCount = 1
            false       // unordered
        );

        emit CrossChainCallSent(messageId, destChainId, targetContract);
    }

    /**
     * @notice Chuyển token Native MTN sang ví nhận ở Chain khác
     * @param destChainId ID của chain đích
     * @param recipient Địa chỉ ví nhận token
     * @param amount Số lượng token cần chuyển
     * @param tip Phí tip cho Relayer
     */
    function sendCrossChainTransfer(
        uint256 destChainId,
        address recipient,
        uint256 amount,
        uint256 tip
    ) external payable returns (bytes32 messageId) {
        uint256 totalRequired = amount + tip;
        require(msg.value >= totalRequired, "Insufficient ETH sent for transfer and tip");

        messageId = IGateway(GATEWAY_ADDRESS).outbound{value: totalRequired}(
            destChainId,
            recipient,
            "",         // Không có calldata
            0,          // assetId = 0 (Native MTN)
            amount,
            tip,
            0,          // gasFee = 0
            1,
            false
        );

        emit CrossChainTransferSent(messageId, destChainId, recipient, amount);
    }
}
