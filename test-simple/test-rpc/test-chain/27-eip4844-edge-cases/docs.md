# 27 - EIP-4844 Edge Cases & Security Boundaries

## 📖 Mục tiêu
Kiểm tra các trường hợp biên, giới hạn an toàn và phòng chống tấn công DoS của **EIP-4844 Blob Transactions**:
1. **Quá giới hạn Blobs/Tx:** Gửi giao dịch mang 7 blobs (vượt quá trần cứng `MAX_BLOBS_PER_TX = 6`).
2. **Cố tình tạo Contract qua BlobTx:** Gửi BlobTx với `To = nil` (vi phạm chuẩn EIP-4844 cấm deploy contract bằng blob tx).
3. **Giả mạo KZG Proof:** Gửi BlobTx với KZG Proof bị sai lệch 1 byte để kiểm tra cơ chế xác minh mật mã toán học KZG point evaluation.

## 🚀 Cách chạy
```bash
cd metanode-suite/test-simple/test-rpc/test-chain/27-eip4844-edge-cases
go run main.go
```

Test xác minh commitment/proof gốc hợp lệ trước khi sửa một byte proof.
Case 2 mã hóa recipient rỗng trong RLP, không dùng địa chỉ zero thay cho `nil`.
Case 3 yêu cầu RPC trả mã `-32000` với `KZG proof verification failed`.
Node trả `invalid transaction` chỉ chứng minh giao dịch bị từ chối, chưa xác định
được bước KZG; cần triển khai binary có bản sửa giữ nguyên lỗi KZG qua lớp RPC.
