# Tool Đo Độ Trễ (Latency) & Demo Chat Giữa 2 User

Công cụ này đã được **gộp thống nhất vào 1 file duy nhất ([main.go](file:///home/abc/nhat/con-chain-v2/metanode-suite/demo-chat/main.go))**, hỗ trợ:
- Tự động nạp cấu hình và các tài khoản có số dư từ `configs/config.json`.
- Đo độ trễ khép kín 2 chu kỳ block (Ping-Pong RTT) qua cả **RPC (HTTP+WebSocket)** và **TCP (Native Socket)**.
- Hỗ trợ cả **Parent Chain (Public Chain)** và **Child Chain (`chain_a`, `chain_b`, ...)**.
- Chế độ **TỰ ĐỘNG (`--mode auto`)**: Tự deploy contract, tự chạy User 1 (receiver) và User 2 (sender), đo latency và in bảng so sánh mà không cần mở 2 terminal!
- Chế độ **CHAT TAY (`--mode chat`)**: Mở console chat tương tác qua lại nếu muốn kiểm tra thủ công.

---

## 🚀 1. Chạy Benchmark Tự Động (Khuyến nghị)

Bạn có thể chạy trực tiếp bằng `go run main.go` hoặc qua script `./run_latency_test.sh`:

### A. Test trên Parent Chain (Public Chain):
```bash
# Test cả RPC và TCP để so sánh:
./run_latency_test.sh --chain parent --proto all --rounds 10

# Hoặc chỉ test RPC:
./run_latency_test.sh --chain parent --proto rpc --rounds 10

# Hoặc chỉ test TCP:
./run_latency_test.sh --chain parent --proto tcp --rounds 10
```

### B. Test trên Child Chain (ví dụ `chain_a`):
```bash
# Test cả RPC và TCP trên child chain:
./run_latency_test.sh --chain chain_a --proto all --rounds 10

# Hoặc chỉ test RPC:
./run_latency_test.sh --chain chain_a --proto rpc --rounds 10

# Hoặc chỉ test TCP:
./run_latency_test.sh --chain chain_a --proto tcp --rounds 10
```

### C. Chạy trực tiếp qua lệnh Go:
```bash
go run main.go --chain parent --proto all --rounds 10
go run main.go --chain chain_a --proto all --rounds 10
```

---

## 📊 Kết Quả Đầu Ra Mẫu
Tool sẽ đo song song 2 chỉ số:
1. **Độ trễ 1-chiều (Client ➡️ Server Mining 1 Block)**: Thời gian từ khi Client gửi Tx tới khi Server đào vào block và trả về receipt/event.
2. **Độ trễ khép kín 2-chiều (Ping-Pong RTT 2 Blocks EVM)**: Thời gian từ khi User 2 gửi PING (Block 1) ➡️ Server đào ➡️ User 1 nhận và phản hồi PONG (Block 2) ➡️ Server đào ➡️ User 2 nhận kết quả.

```text
═════════════════════════════════════════════════════════════
  ⚖️  BẢNG SO SÁNH ĐỘ TRỄ: RPC (HTTP+WS) vs TCP (NATIVE)
═════════════════════════════════════════════════════════════
  Chỉ số thống kê          | RPC (HTTP + WS)  | TCP (Native)    
  ─────────────────────────┼──────────────────┼─────────────────
  Thành công               | 10/10            | 10/10           
  Server 1-Block (Avg)     | 38 ms            | 36 ms           
  Ping-Pong RTT (Avg)      | 78 ms            | 75 ms           
  RTT Nhanh nhất (Min)     | 76 ms            | 75 ms           
  RTT Lâu nhất (Max)       | 81 ms            | 75 ms           
  RTT Phân vị P50          | 81 ms            | 75 ms           
  RTT Phân vị P95          | 81 ms            | 75 ms           
═════════════════════════════════════════════════════════════
```

---

## 💬 2. Chạy Chế Độ Chat Tay Thủ Công (Nếu Muốn)

Nếu bạn muốn mở 2 terminal để gõ tin nhắn thủ công qua lại:

### Terminal 1 (User 1 - Người nhận/lắng nghe):
```bash
go run main.go --chain parent --proto rpc --mode chat --user 1
```

### Terminal 2 (User 2 - Người gửi):
```bash
go run main.go --chain parent --proto rpc --mode chat --user 2
```
*(Nếu muốn chat qua TCP, chỉ cần đổi `--proto rpc` thành `--proto tcp`)*.

---

## ⚙️ Các Cờ Tùy Chọn (Flags)
- `--chain`: Tên chain cần test (`parent`, `chain_a`, `chain_b`...). Mặc định: `parent`.
- `--proto`: Giao thức (`all`, `rpc`, `tcp`). Mặc định: `all`.
- `--rounds`: Số vòng ping-pong đo độ trễ. Mặc định: `10`.
- `--contract`: Chỉ định địa chỉ contract thủ công (nếu không truyền, tool tự động deploy hoặc đọc từ cache `.contract_<chain>.txt`).
- `--redeploy`: Bắt buộc deploy lại contract mới thay vì dùng cache.
- `--config`: Đường dẫn tới file `config.json` (mặc định: `../configs/config.json`).
