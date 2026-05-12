# Test Node Recovery Gap

Công cụ này dùng để kiểm tra khả năng phục hồi tự động (Auto-Recovery) và đồng bộ của một node bất kỳ sau khi nó bị ngắt kết nối/tắt đi trong một khoảng thời gian (khoảng hổng `Gap Epoch`). Kịch bản cũng tự động kiểm tra xem Node có bị lệch Hash so với phần còn lại của mạng sau khi hồi phục hay không.

## 🛠 Cách hoạt động (7 Bước)

1. Tắt node được chọn bằng `mtn-orchestrator.sh`.
2. Chạy ngầm công cụ spam giao dịch (`rpc-tcp-simple.sh`) để đẩy mạng tiến tới.
3. Chờ đợi mạng sinh ra đủ số lượng Epoch như yêu cầu (`Gap Epoch`).
4. Tạm ngưng spam, bật lại node.
5. Gọi `Hash Checker` (chạy 30 giây) để xem Node có đồng bộ đúng stateRoot, txRoot... và không bị lệch Hash không.
6. Sau khi node hồi phục, tiếp tục bật lại công cụ Spam giao dịch (Stress Test).
7. Gọi `Hash Checker` (chạy thêm 40 giây) để đảm bảo dưới áp lực cao, mạng vẫn ổn định và không xảy ra phân chia chuỗi (Fork/Chain Broken).

---

## 🚀 Hướng dẫn chạy

### 1. Chạy với cấu hình mặc định

Mặc định công cụ sẽ test **tắt Node 1**, chờ **tạo khoảng hổng 3 Epochs** trước khi bật lại, và chạy test **1 lần**.

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
./test-node-recovery-gap.sh 1 3 10
```

---

## 🛑 Dừng test khẩn cấp

Trong quá trình test đang chạy, nếu bạn muốn ngắt giữa chừng, chỉ cần bấm **`Ctrl + C`**.
Kịch bản đã được cài đặt `trap`, nó sẽ tự động dọn dẹp các tiến trình bắn giao dịch đang chạy ngầm, không để lại rác trên máy.
