# 🚀 Script: `rpc-tcp-simple.sh`

Script này dùng để chạy liên tiếp 2 bộ test **RPC** và **TCP** (dựa trên cấu hình `config-local.json` và `data.json`).

## 🛠️ Hướng dẫn sử dụng

Vào thư mục chứa script:
```bash
cd ~/nhat/con-chain-v2/tool-test/scripts
```

### 1. Chạy bình thường (1 lần)
Script sẽ lần lượt chạy lệnh `go run` của test RPC, sau đó đến TCP rồi tự động kết thúc.
```bash
./rpc-tcp-simple.sh
```

### 2. Chạy lặp vô hạn (Loop)
Thêm cờ `--loop` để script chạy xoay vòng liên tục chu trình test RPC ➡️ TCP. Thích hợp để test độ ổn định (stability test) hoặc kiểm tra bug phát sinh khi spam.
```bash
./rpc-tcp-simple.sh --loop
```

> 🛑 **Lưu ý:** Để dừng vòng lặp, nhấn tổ hợp phím **`Ctrl + C`** trên terminal.
