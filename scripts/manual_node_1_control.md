# Hướng dẫn Điều khiển Node Thủ công (Node 1)

Dưới đây là các lệnh bash thuần túy để điều khiển Node 1 mà không cần dùng thông qua `mtn-orchestrator.sh`. Các lệnh này trích xuất trực tiếp logic start/stop an toàn từ orchestrator.

## 🛑 1. Dừng Node An toàn (Graceful Shutdown)

**Luôn sử dụng `SIGTERM`** để node có thời gian tự xả buffer và đóng PebbleDB/NOMT. Tuyệt đối không dùng `kill -9` (trừ khi node bị treo hoàn toàn).

```bash
# 1. Gửi tín hiệu tắt an toàn cho tiến trình Node 1
pgrep -f "simple_chain.*config-master-node1.json" | xargs kill -TERM

# 2. Đợi khoảng 2-5 giây để node xả dữ liệu. 
# (Tùy chọn) Xóa tmux session nếu nó không tự đóng:
tmux kill-session -t go-master-1 2>/dev/null
```

---

## 🚀 2. Khởi động Node

Đây là toàn bộ quy trình thiết lập môi trường và chạy binary y hệt như orchestrator.

### Cách 2A: Chạy ẩn trong Background (Lưu log ra file)
Giống cách script orchestrator chạy thật.

```bash
# 1. Chuyển vào thư mục chứa code
cd /home/abc/nhat/con-chain-v2/metanode/execution/cmd/simple_chain

# 2. Cấu hình biến môi trường
ulimit -n 100000
export RUST_BACKTRACE=full
export GOTRACEBACK=crash
export GOTOOLCHAIN=go1.23.5
export GOMEMLIMIT=500MiB
export XAPIAN_BASE_PATH="/home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1/xapian"
export MVM_LOG_DIR="/home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1"

# 3. Chạy Node và đẩy log ra file (chạy ẩn)
./simple_chain -config=config-master-node1.json --pprof-addr=127.0.0.1:16061 >> "/home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1/go-master-stdout.log" 2>&1 &
```

### Cách 2B: Chạy trực tiếp trên Terminal (Xem log Realtime)
Dành cho lúc bạn muốn debug trực tiếp trên cửa sổ hiện tại. Bấm `Ctrl+C` để tắt an toàn.

```bash
# 1. Chuyển vào thư mục chứa code
cd /home/abc/nhat/con-chain-v2/metanode/execution/cmd/simple_chain

# 2. Cấu hình biến môi trường
ulimit -n 100000
export RUST_BACKTRACE=full
export GOTRACEBACK=crash
export GOTOOLCHAIN=go1.23.5
export GOMEMLIMIT=500MiB
export XAPIAN_BASE_PATH="/home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1/xapian"
export MVM_LOG_DIR="/home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1"

# 3. Chạy Node trực tiếp
./simple_chain -config=config-master-node1.json --pprof-addr=127.0.0.1:16061
```

---

## 🧹 3. Dọn dẹp Sockets (Nếu bị lỗi cổng)

Khi node crash nặng (ví dụ mất điện), file socket cũ có thể chưa được xóa. Bạn cần xóa chúng trước khi khởi động lại:

```bash
rm -f /tmp/mtn_executor_1.sock
rm -f /tmp/mtn_master_1.sock
rm -f /tmp/mtn_tx_1.sock
```
