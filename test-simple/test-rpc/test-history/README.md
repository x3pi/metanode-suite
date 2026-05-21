# Test History Tool

Tool này kiểm tra xem Node có thể truy xuất chính xác dữ liệu lịch sử (Archive State) hay không. Nó tiến hành so sánh số dư (Balance) và Nonce của cùng một ví kiểm thử tại 2 mốc block khác nhau.

**Lưu ý**: Chỉ chạy đúng với cấu hình `"state_backend": "mpt"`. Với `"nomt"`, Node sẽ luôn trả về state hiện tại.


pkill -f 'test-rpc/test-history'
pkill -f 'block_hash_checker'

---

## 🛠 Cơ chế hoạt động chi tiết

1. **Kết nối dự phòng (RPC Failover)**:
   - Tool sử dụng struct `FailoverClient` để quản lý danh sách các node RPC (`rpc_urls`).
   - Nếu node hiện tại bị sập hoặc gặp lỗi kết nối, client tự động kết nối (reconnect) và chuyển hướng yêu cầu sang node khác trong danh sách mà không làm gián đoạn hay crash tool.

2. **Chốt mốc Block A**:
   - Gửi giao dịch **Tx 1** và đợi cho đến khi giao dịch được đóng gói thành công. Mốc này được gọi là **Block A**.
   - Ngay lập tức gọi RPC truy vấn `eth_getBalance` và `eth_getTransactionCount` tại Block A để lưu trữ số dư và nonce thực tế vào RAM làm dữ liệu đối chiếu (`savedBalanceA` và `savedNonceA`).

3. **Chạy tới Block B**:
   - **Chạy nhanh (mặc định)**: Gửi tiếp **Tx 2** để lập tức sinh ra **Block B** chứa trạng thái mới đã biến đổi.
   - **Chờ block (`-wait N`)**: Liên tục gửi các giao dịch kích block cho tới khi chiều cao blockchain tăng thêm ít nhất `N` block để đạt mốc **Block B = Block A + N**.

4. **Đối chiếu lịch sử**:
   - Truy vấn số dư và Nonce lịch sử tại mốc **Block A**: `eth_getBalance(BlockA)`, `eth_getTransactionCount(BlockA)`.
   - Truy vấn số dư và Nonce hiện tại tại mốc **Block B**: `eth_getBalance(BlockB)`, `eth_getTransactionCount(BlockB)`.
   - Đối chiếu:
     - **Tách biệt**: Giá trị tại Block A phải **khác** tại Block B (nếu giống nhau nghĩa là bị rò rỉ trạng thái mới nhất).
     - **Chính xác**: Giá trị lịch sử truy vấn lại ở Block A phải **khớp hoàn toàn** với giá trị đã lưu ở RAM ở Bước 2.

5. **Ghi nhận Log & Dừng khẩn cấp**:
   - Kết quả từng vòng kiểm tra được ghi vào file local `history_records.json`. Log này tự động cắt tỉa (prune) giữ lại tối đa **1000 bản ghi** mới nhất để tránh tràn bộ nhớ đĩa.
   - Nếu phát hiện bất kỳ lỗi lệch trạng thái lịch sử nào, tool sẽ tạo file cờ `/tmp/MTN_CHAIN_ERROR_STOP` để báo động cho pipeline test (`simple_test.sh` hoặc `auto_test.sh`) dừng khẩn cấp lập tức.

---

## ⚙️ Cấu hình (`config-local.json`)

```json
{
    "rpc_url": "http://127.0.0.1:8545",
    "rpc_urls": [
        "http://127.0.0.1:8545",
        "http://127.0.0.1:8547",
        "http://127.0.0.1:8548",
        "http://127.0.0.1:8549",
        "http://127.0.0.1:8550"
    ],
    "private_key": "28f0ad246c39...",
    "chain_id": 991
}
```

*Nếu `"rpc_urls"` trống, hệ thống sẽ tự động sử dụng duy nhất địa chỉ trong `"rpc_url"` để tương thích ngược.*

---

## 🚀 Hướng dẫn lệnh chạy

### 1. Kiểm tra một lần (Chế độ mặc định)

Lập tức chạy kiểm tra lịch sử state 1 lần rồi thoát.

```bash
go run main.go -config config-local.json
```

### 2. Kiểm tra khoảng cách xa (Test Pruning)

Đợi mạng lưới chạy thêm `wait` block trước khi đối chiếu mốc B với mốc lịch sử A.

```bash
# Đợi 50 block rồi mới check
go run main.go -config config-local.json -wait 50
```

### 3. Kiểm tra liên tục (Chế độ chạy ngầm)

Chạy lặp lại liên tục vô hạn, tự động cập nhật nhật ký và ghi cờ lỗi dừng hệ thống khi phát hiện sai số.

```bash
go run main.go -config config-local.json -wait 5 -loop 
go run main.go -config config-local.json -wait 5 -target-node 1

```

### 4. Kiểm tra lưu và xác minh lịch sử động (Test Recovery & Snapshot)

Khi chạy các kịch bản test tắt node hoặc khôi phục node (ví dụ: `test-node-recovery-gap.sh` hoặc `test-snapshot.sh`), tool hỗ trợ cơ chế lưu checkpoint trước khi tắt và xác minh trực tiếp trên node sau khi khởi động lại.

#### A. Lưu trạng thái trước khi tắt node (`-action save`)

Gửi 1 giao dịch và xuất thông tin số dư/nonce tại block đó ra file JSON chỉ định:

```bash
go run main.go -config config-local.json -action save -file /tmp/pending_check_1.json
```

#### B. Xác minh trực tiếp trên node sau khi phục hồi (`-action verify`)

Đọc file JSON đã lưu, chờ node mục tiêu (được cấu hình bằng `-target-node`) hoạt động và đồng bộ vượt qua block cũ, sau đó truy vấn trực tiếp và đối chiếu lịch sử từ chính node đó để đảm bảo trả về dữ liệu lịch sử chính xác:

```bash
go run main.go -config config-local.json -action verify -file /tmp/pending_check_1.json -target-node 1
```

*(Tham số `-target-node` có thể nhận Node ID `0` đến `4` hoặc URL RPC trực tiếp).*