# 25-eip4844-blob-tx (EIP-4844 Shard Blob Transaction)

### 1. Mục đích
Kiểm tra khả năng xử lý của Node và Block-STM khi tiếp nhận và thực thi giao dịch **EIP-4844 Blob Transaction (`TxType = 0x03`)**.

### 2. Cách hoạt động
1. Khởi tạo dữ liệu Blob (128 KB KZG Blob format).
2. Tính toán KZG Commitment và KZG Proof tương ứng.
3. Sinh Versioned Hash từ Commitment (`0x01 || SHA256(Commitment)[1:]`).
4. Đóng gói giao dịch `types.BlobTx` mang theo `Sidecar`, `BlobHashes`, và `BlobFeeCap`.
5. Ký giao dịch bằng `CancunSigner` / `PragueSigner` và gửi qua JSON-RPC `eth_sendRawTransaction`.
6. Chờ receipt và xác minh `TxType == 0x03`, `Status == 1`, `BlobGasUsed > 0`.

### 3. Kết quả kỳ vọng
- Node bóc tách Sidecar hợp lệ lưu vào `blob_store`.
- Transaction header được đưa vào block và thực thi thành công mà không bị revert hay panic.
- Receipt trả về đầy đủ các trường EIP-4844 (`BlobGasUsed`, `BlobGasPrice`, `Type: 0x03`).
