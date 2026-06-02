# 🚀 Script: `rpc-tcp-simple.sh`

Script này dùng để chạy liên tiếp 2 bộ test **RPC** và **TCP** (dựa trên cấu hình `config-local.json` và `data.json`).

## 🛠️ Hướng dẫn sử dụng

Vào thư mục chứa script:

```bash
cd ~/nhat/con-chain-v2/tool-test/scripts
```

### 1. Danh sách Port của các Node (Localhost)

Để thuận tiện cho việc test, bạn có thể chọn nhanh Node target thông qua cờ `--node 0-4`:

- **Node 0:** RPC `http://127.0.0.1:8545` | TCP `127.0.0.1:4201` (Default)
- **Node 1:** RPC `http://127.0.0.1:8547` | TCP `127.0.0.1:6201`
- **Node 2:** RPC `http://127.0.0.1:8548` | TCP `127.0.0.1:6211`
- **Node 3:** RPC `http://127.0.0.1:8549` | TCP `127.0.0.1:6221`
- **Node 4:** RPC `http://127.0.0.1:8550` | TCP `127.0.0.1:6241`

### 2. Chạy bình thường (1 lần)

Script sẽ lần lượt chạy lệnh `go run` của test RPC, sau đó đến TCP rồi tự động kết thúc.

```bash
./rpc-tcp-simple.sh
```

### 3. Chọn Node để chạy tự động (Mới)

Bạn có thể dùng cờ `--node` để script tự động thiết lập đúng cặp URL/Port của Node đó cho cả RPC và TCP test:

```bash
# Test Node 2 (RPC :8548 và TCP :6211)
./rpc-tcp-simple.sh --node 2
./rpc-tcp-simple.sh --node 1
./rpc-tcp-simple.sh --node 0

# Test Node 4 (RPC :8550 và TCP :6241)
./rpc-tcp-simple.sh --node 4
```

### 4. Ghi đè URL tuỳ chỉnh (Mới)

Trong trường hợp chạy trên các máy khác nhau hoặc cấu hình đặc biệt, bạn có thể ghi đè thủ công từng loại URL:

```bash
./rpc-tcp-simple.sh --rpc-url http://127.0.0.1:8545 --tcp-url 127.0.0.1:4201
```

### 5. Chạy lặp vô hạn (Loop)

Thêm cờ `--loop` để script chạy xoay vòng liên tục chu trình test RPC ➡️ TCP. Thích hợp để test độ ổn định (stability test) hoặc kiểm tra bug phát sinh khi spam.

```bash
./rpc-tcp-simple.sh --loop --node 1
./rpc-tcp-simple.sh --loop --node 4
./rpc-tcp-simple.sh --loop --rpc-url http://127.0.0.1:8550 --tcp-url 127.0.0.1:6241
```

> 🛑 **Lưu ý:** Để dừng vòng lặp, nhấn tổ hợp phím **`Ctrl + C`** trên terminal.
