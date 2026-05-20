# Test Node Recovery Gap (Script Gốc)

Công cụ này hiện đóng vai trò là **Script Chính (Main Script)** để kiểm tra toàn bộ vòng đời của mạng Metanode, bao gồm khởi tạo mạng, test giao dịch cơ bản, và kiểm tra khả năng phục hồi tự động (Auto-Recovery) của một node bất kỳ sau khi nó bị ngắt kết nối (tạo khoảng hổng `Gap Epoch`).

## 🛠 Cách hoạt động (8 Bước)

1. **Khởi tạo & Test Cơ Bản**: Tự động gọi `simple_test.sh` để setup mạng từ con số 0 và chạy các bài test cơ bản ban đầu (Bước 1 -> 8).
2. **Kiểm tra Điều kiện**: Đảm bảo mạng đã chạy ít nhất qua Epoch 1.
3. **Tắt Node**: Tắt node được chọn (mặc định là Node 1) bằng `mtn-orchestrator.sh`.
4. **Tạo Gap (Bắn giao dịch ngầm)**: Chạy ngầm tool `send_tx_native` (với thông số `--count 20000 --parallel_native=true`) để đẩy Epoch của các node khỏe lên cao.
5. **Đợi Gap Epoch**: Chờ đợi mạng sinh ra đủ số lượng Epoch như yêu cầu (`Gap Epoch`) rồi tạm ngưng spam giao dịch.
6. **Khởi động lại Node**: Bật lại node bị tắt, cho phép nó kết nối lại mạng để tải State và Block còn thiếu.
7. **Kiểm tra Đồng bộ (Hash Checker)**: Chạy công cụ kiểm tra Hash trong 30 giây để xác nhận Node đã hồi phục thành công và khớp Hash với mạng.
8. **Stress Test & Kiểm tra lại**: Tiếp tục bật lại công cụ `send_tx_native` để dội tải vào mạng, đồng thời chạy Hash Checker thêm 40 giây để đảm bảo dưới áp lực cao, node phục hồi không bị rớt nhịp hay phân nhánh.

---

## 🚀 Hướng dẫn chạy

### 1. Chạy với cấu hình mặc định

Mặc định công cụ sẽ test **tắt Node 1**, chờ **tạo khoảng hổng 1 Epoch** trước khi bật lại, và chạy test **1 lần**.

```bash
cd /home/abc/nhat/con-chain-v2/tool-test/scripts
./test-node-recovery-gap.sh
```

### 2. Chạy với Node, Epoch Gap và Số lần lặp tùy chọn

Bạn có thể truyền trực tiếp `Node ID`, số `Gap Epoch` và số `Vòng Lặp` vào:

```bash
# Cú pháp:
# ./test-node-recovery-gap.sh <NodeID> <GapEpoch> <Số lần lặp>

# Ví dụ 1: Tắt Node 2, chờ 5 Epochs, test 1 lần
./test-node-recovery-gap.sh 2 5 1

# Ví dụ 2: Tắt Node 1, chờ 3 Epochs, lặp lại test 10 lần liên tục
./test-node-recovery-gap.sh 1 3 3
```

---

## 🛑 Dừng test khẩn cấp

Trong quá trình test đang chạy, nếu bạn muốn ngắt giữa chừng, chỉ cần bấm **`Ctrl + C`**.
Kịch bản đã được cài đặt `trap`, nó sẽ tự động dọn dẹp các tiến trình bắn giao dịch đang chạy ngầm, không để lại rác trên máy.
