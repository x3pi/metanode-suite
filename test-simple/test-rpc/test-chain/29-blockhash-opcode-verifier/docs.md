# 29 - Opcode BLOCKHASH Verification (TEE Packaging B1 Context)

## 📖 Mục tiêu
Kiểm tra tính toàn vẹn và bảo mật của Opcode `BLOCKHASH (0x40)` trong môi trường TEE Packaging:
1. **Triển khai Verifier Contract:** Deploy contract gọi opcode `BLOCKHASH(blockNumber)` trả về hash.
2. **Khớp hash lịch sử:** Truy vấn `BLOCKHASH(block.number - 1)` và đối chiếu 1:1 với RPC Block Header Hash.
3. **Quy tắc bảo mật thời gian:** Truy vấn block hiện tại hoặc tương lai (`>= block.number`) -> Trả về `0x000...0`.
4. **Cửa sổ 256 Block:** Truy vấn block quá 256 block trước -> Trả về `0x000...0`.

## 🚀 Cách chạy
```bash
cd metanode-suite/test-simple/test-rpc/test-blockstm/29-blockhash-opcode-verifier
go run main.go
```
