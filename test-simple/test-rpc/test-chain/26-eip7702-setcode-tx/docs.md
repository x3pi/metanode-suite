# 26-eip7702-setcode-tx (EIP-7702 SetCode Transaction)

### 1. Mục đích
Kiểm tra khả năng tiếp nhận, xác thực chữ ký authorization, cập nhật delegation code và thực thi giao dịch **EIP-7702 SetCode Transaction (`TxType = 0x04`)**.

### 2. Cách hoạt động
1. Chuẩn bị EOA Authority (tài khoản ủy quyền) và Sender (tài khoản gửi tx / trả gas).
2. Tạo bản ghi `SetCodeAuthorization` chỉ định địa chỉ `delegateContract` và `nonce` hiện tại của Authority.
3. Ký `SetCodeAuthorization` bằng Private Key của Authority với `types.SignSetCode`.
4. Đóng gói giao dịch `types.SetCodeTx` chứa `AuthList` đã ký.
5. Ký `types.SetCodeTx` bằng `PragueSigner` và gửi qua JSON-RPC `eth_sendRawTransaction`.
6. Chờ receipt và xác minh `TxType == 0x04`, `Status == 1`.
7. Kiểm tra mã thực thi (code) của tài khoản Authority (được gắn designator `0xef0100 || delegate`).

### 3. Kết quả kỳ vọng
- RPC và Mempool giải mã và chấp nhận transaction type `0x04`.
- Node xác thực thành công chữ ký trên từng authorization tuple trong `AuthList`.
- Giao dịch thực thi thành công trong Block-STM mà không xảy ra conflict hay panic.
- Receipt trả về đúng `TxType = 0x04` và `Status = 1`.
