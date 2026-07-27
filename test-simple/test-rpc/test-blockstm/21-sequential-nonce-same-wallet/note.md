# Hướng dẫn chạy Test: 21-sequential-nonce-same-wallet

Bài test này gửi liên tục nhiều giao dịch từ **MỘT ví duy nhất** (nonce tăng dần liên tục) gọi hàm update của Smart Contract để kiểm tra tính đúng đắn và khả năng sắp xếp thứ tự nonce của Block-STM.

---

## 🚀 Các câu lệnh chạy

### 1. Chạy mặc định (10 giao dịch)
```bash
go run main.go
```

### 2. Chạy với số lượng giao dịch tùy chỉnh (Ví dụ: 100 txs)
```bash
go run main.go -num 100
```

### 3. Chạy với tham số đầy đủ (Config & Keys riêng)
```bash
go run main.go -num 500 -config ../config.json -keys ../../../../test_tps/gen_spam_keys/generated_keys.json
```

---

## 📋 Đốm tham số (Flags)
- `-num`: Số lượng transaction liên tiếp gửi từ 1 ví (mặc định: `10`)
- `-config`: Đường dẫn file cấu hình RPC & ABIs (mặc định: `../config.json`)
- `-keys`: Đường dẫn file chứa Private Keys (mặc định: `../../../../test_tps/gen_spam_keys/generated_keys.json`)