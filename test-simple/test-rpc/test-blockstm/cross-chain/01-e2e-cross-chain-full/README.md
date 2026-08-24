# 🌐 E2E Cross-Chain Full Test Suite (P0 ➔ P8)

Thư mục chứa 2 chế độ kiểm thử chu trình liên chuỗi toàn diện:

---

## 📂 2 Chế Độ Chạy:

### 1. Chế độ Test Runner truyền thống (All-in-One Manual):
File `main.go` tự thực hiện tuần tự từng bước từ A ➔ Public ➔ B:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/01-e2e-cross-chain-full
go run .
```

---

### 2. Chế độ Tích Hợp Autonomous Relayer Network (Khuyên Dùng):
File `relayer-mode/main.go` kích hoạt **tiến trình Relayer chạy ngầm độc lập**. Người dùng **chỉ gửi 1 giao dịch duy nhất trên Chain A**, Relayer tự động bắt và chuyển sang Chain B:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/01-e2e-cross-chain-full/relayer-mode
go run .
```
