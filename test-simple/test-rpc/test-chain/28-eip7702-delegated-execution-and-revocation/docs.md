# 28 - EIP-7702 Delegated Execution & Revocation

## 📖 Mục tiêu
Kiểm tra vòng đời đầy đủ của **EIP-7702 SetCode Transactions**:
1. **Deploy Target Contract:** Triển khai một contract chức năng (chẳng hạn logic tính toán / lưu trữ).
2. **Ký Ủy quyền (Delegation):** EOA B ký Authorization tuple ủy quyền cho contract trên.
3. **Thực thi qua EOA:** Caller gửi giao dịch gọi vào địa chỉ EOA B -> EVM tải và thực thi logic của target contract trên ngữ cảnh tài khoản của EOA B.
4. **Thu hồi ủy quyền (Revocation):** EOA B ký Authorization trỏ về `0x0000000000000000000000000000000000000000` -> Mã bytecode tại EOA B được xóa sạch về `0x` (rỗng).

## 🚀 Cách chạy
```bash
cd metanode-suite/test-simple/test-rpc/test-blockstm/28-eip7702-delegated-execution-and-revocation
go run main.go
```
