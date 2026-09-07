# 27 - EIP-4844 Edge Cases & Security Boundaries

## 📖 Mục tiêu
Kiểm tra các trường hợp biên, giới hạn an toàn và phòng chống tấn công DoS của **EIP-4844 Blob Transactions**:
1. **Quá giới hạn Blobs/Tx:** Gửi giao dịch mang 7 blobs (vượt quá trần cứng `MAX_BLOBS_PER_TX = 6`).
2. **Cố tình tạo Contract qua BlobTx:** Gửi BlobTx với `To = nil` (vi phạm chuẩn EIP-4844 cấm deploy contract bằng blob tx).
3. **Giả mạo KZG Proof:** Gửi BlobTx với KZG Proof bị sai lệch 1 byte để kiểm tra cơ chế xác minh mật mã toán học KZG point evaluation.

## 🚀 Cách chạy
```bash
cd metanode-suite/test-simple/test-rpc/test-blockstm/27-eip4844-edge-cases
go run main.go
```
