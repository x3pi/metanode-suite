# 🚀 Script: `rpc-tcp-simple.sh`

Script này dùng để chạy liên tiếp 2 bộ test **RPC** và **TCP** (dựa trên cấu hình `config-local.json` và `data.json`).

## 🛠️ Hướng dẫn sử dụng

Vào thư mục chứa script:
```bash
cd ~/nhat/con-chain-v2/tool-test/scripts
```

### 1. Danh sách Port của các Node (Localhost)
Để thuận tiện cho việc test, bạn có thể tham khảo bảng port mặc định của cụm 4 node:
- **Node 1:** RPC `http://127.0.0.1:8757` | TCP `127.0.0.1:4201`
- **Node 2:** RPC `http://127.0.0.1:10747` | TCP `127.0.0.1:6201`
- **Node 3:** RPC `http://127.0.0.1:10749` | TCP `127.0.0.1:6211`
- **Node 4:** RPC `http://127.0.0.1:10750` | TCP `127.0.0.1:6221`

### 2. Chạy bình thường (1 lần)
Script sẽ lần lượt chạy lệnh `go run` của test RPC, sau đó đến TCP rồi tự động kết thúc.
```bash
./rpc-tcp-simple.sh
```

### 3. Ghi đè URL tuỳ chỉnh (Mới)
Bạn có thể trỏ cả bộ test TCP và RPC tới một URL khác (không cần phải sửa các file config) bằng cờ `--url`. Tool sẽ tự động áp dụng đường dẫn mới cho toàn bộ các bước kiểm thử.
```bash
./rpc-tcp-simple.sh --url=http://127.0.0.1:8757
```

### 4. Chạy lặp vô hạn (Loop)
Thêm cờ `--loop` để script chạy xoay vòng liên tục chu trình test RPC ➡️ TCP. Thích hợp để test độ ổn định (stability test) hoặc kiểm tra bug phát sinh khi spam. Có thể dùng kết hợp cả `--url` và `--loop`.
```bash
./rpc-tcp-simple.sh --loop --url=http://127.0.0.1:8757
```

> 🛑 **Lưu ý:** Để dừng vòng lặp, nhấn tổ hợp phím **`Ctrl + C`** trên terminal.
