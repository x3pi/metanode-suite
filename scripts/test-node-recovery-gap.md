# Test Node Recovery Gap Pipeline (`test-node-recovery-gap.sh`)

Kịch bản này được thiết kế để kiểm tra khả năng phục hồi dữ liệu tự động (Auto-Recovery/State Sync) của một node trong mạng lưới Metanode sau khi bị tắt kết nối (tạo khoảng hổng dữ liệu `Gap Epoch`). Ngoài ra, script còn tích hợp kiểm tra độ chính xác của **truy vấn lịch sử trạng thái (Historical State)** trên node phục hồi.

---

## 🛠 Quy trình kiểm thử chi tiết (8 Bước)

### 1. Khởi tạo mạng & Kiểm tra cơ bản
Script gọi `./simple_test.sh` để khởi tạo mạng blockchain từ đầu (nếu chưa chạy) và thực hiện các bước test giao dịch cơ bản ban đầu để đảm bảo mạng khỏe mạnh trước khi chạy test khôi phục.

### 2. Kiểm tra điều kiện Epoch
Đảm bảo mạng lưới đã bắt đầu hoạt động và chạy vượt qua Epoch 1.

### 3. Lưu checkpoint trạng thái lịch sử
Trước khi tắt node mục tiêu (`TARGET_NODE`):
- Script chạy công cụ `test-history` ở hành động `-action save`.
- Gửi 1 giao dịch thành công để thay đổi số dư/nonce.
- Lấy thông tin số dư và nonce của ví tại block mới nhất đó (Block A).
- Lưu thông tin checkpoint này vào `/tmp/pending_check_${TARGET_NODE}.json`.

### 4. Tắt Node mục tiêu
Sử dụng script quản trị để tắt tiến trình của node cần test (`TARGET_NODE`, ví dụ: Node 1).

### 5. Tạo khoảng hổng Epoch (Gap Epoch)
- Bật công cụ `tps_blast_cc` để bắn lượng lớn giao dịch (mặc định 20,000 giao dịch) ngầm vào mạng lưới.
- Việc bắn giao dịch này được định tuyến trực tiếp vào một node khỏe mạnh khác (`SPAM_NODE`) bằng flag `--target-node` nhằm ép mạng lưới tăng trưởng Epoch trong khi node mục tiêu đang tắt.

### 6. Đợi mạng đạt mốc Gap Epoch
Đợi cho đến khi Epoch hiện tại của các node đang chạy vượt qua mốc Epoch đích (`TARGET_EPOCH = START_EPOCH + GAP_EPOCH`). Lúc này, node bị tắt đã bị tụt hậu so với mạng lưới một khoảng nhất định. Script sau đó sẽ dừng bắn giao dịch tạm thời.

### 7. Khởi động lại Node & Xác minh lịch sử trạng thái
- Script khởi động lại node mục tiêu (`TARGET_NODE`).
- Đợi 5 giây để đảm bảo node không bị crash loop, sau đó đợi 10 giây cho node bắt kịp mạng lưới và tải về dữ liệu còn thiếu thông qua cơ chế Recovery.
- Thực hiện xác minh dữ liệu lịch sử bằng lệnh:
  ```bash
  go run main.go -config config-local.json -action verify -file /tmp/pending_check_${TARGET_NODE}.json -target-node $TARGET_NODE
  ```
- Node vừa phục hồi **bắt buộc phải trả về chính xác** số dư và nonce lịch sử tại Block A giống hệt như checkpoint đã lưu trước đó. Nếu dữ liệu lịch sử bị sai lệch hoặc bị rò rỉ dữ liệu mới (state mới ghi đè block cũ), kiểm thử sẽ dừng lập tức với mã lỗi 1.

### 8. Stress Test sau phục hồi & Kiểm tra Hash
- Để kiểm tra độ ổn định của node phục hồi dưới tải cao, script bắn song song 2 luồng TPS (mỗi luồng 20,000 giao dịch): một luồng hướng vào node vừa hồi phục (`TARGET_NODE`), một luồng hướng vào node khác (`SPAM_NODE`).
- Nếu bất kỳ tiến trình bắn giao dịch nào gặp lỗi hoặc panic, script sẽ dừng và xuất log chi tiết ngay.
- Đồng thời, `block_hash_checker` chạy để xác minh tính đồng thuận (không có node nào bị lệch hash block).

---

## 🚀 Hướng dẫn chạy

### 1. Chạy với cấu hình mặc định
Mặc định sẽ test tắt **Node 1**, tạo khoảng trống dữ liệu **1 Epoch**, và chạy **1 vòng lặp**:
```bash
cd scripts/
./test-node-recovery-gap.sh
```

### 2. Chạy với tham số tùy chọn
Cú pháp:
```bash
./test-node-recovery-gap.sh <NodeID> <GapEpoch> <Số lần lặp>
```

Ví dụ:
- Tắt Node 2, chờ 5 Epochs, test 1 lần:
  ```bash
  ./test-node-recovery-gap.sh 2 5 1
  ```
- Tắt Node 1, chờ 3 Epochs, lặp lại test 3 lần liên tục:
  ```bash
  ./test-node-recovery-gap.sh 1 3 3
  ```
