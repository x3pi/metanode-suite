# Test Snapshot Pipeline (`test-snapshot.sh`)

Kịch bản này dùng để kiểm tra tự động tính năng khôi phục một Node từ một bản Snapshot có sẵn của Node khác trong mạng lưới Metanode. Nó đảm bảo node khôi phục từ snapshot có thể đồng bộ nhanh chóng, chạy giao dịch bình thường, khớp hash block với mạng đồng thuận, và trả về chính xác **lịch sử trạng thái (Historical State)**.

---

## 🛠 Quy trình kiểm thử chi tiết trong mỗi vòng lặp

### 1. Khởi động Hash Checker giám sát ngầm
Bật `block_hash_checker` để theo dõi liên tục mã băm (hash) block giữa các node trong mạng, đảm bảo phát hiện lệch hash ngay khi có lỗi phân nhánh.

### 2. Chạy TPS trước Snapshot
Chạy `tps_blast_cc` để gửi một lượng lớn giao dịch vào mạng lưới (mặc định 20,000 giao dịch). Quá trình này giúp tích lũy dữ liệu block và thay đổi state trên mạng lưới trước khi thực hiện snapshot/restore.

### 3. Lưu checkpoint & Khôi phục Node từ Snapshot (Restore Node)
- Chạy công cụ `test-history` ở chế độ `-action save` để thực hiện một giao dịch thay đổi số dư/nonce, ghi nhận lại số dư và nonce của ví tại mốc block đó (Block A), rồi lưu checkpoint vào `/tmp/pending_check_${NODE_ID}.json`.
- Sử dụng script `restore_node.sh` để xóa dữ liệu hiện tại của node cần test (`NODE_ID`) và khôi phục nó bằng cách sao chép dữ liệu thư mục snapshot từ Node 4. (Lưu ý: `NODE_ID` sẽ được luân phiên tự động 0 -> 1 -> 2 -> 3 qua các vòng lặp).
- Quá trình này mô phỏng việc khôi phục một node trống dữ liệu sử dụng bản snapshot đáng tin cậy.

### 4. Chờ các node đồng bộ block & Ổn định
- Theo dõi log của hệ thống để đảm bảo khoảng cách 'CHÊNH' block giữa các node giảm dần về 0.
- Đợi thêm 5 giây cho hệ thống đóng gói các khối ổn định và lưu state.
- **Xác minh lịch sử trạng thái (Historical State Verification):** Ngay sau khi node đồng bộ xong, script thực hiện đối chiếu dữ liệu lịch sử ngay lập tức:

  ```bash
  go run main.go -config config-local.json -action verify -file /tmp/pending_check_${NODE_ID}.json -target-node $NODE_ID
  ```

  Node phục hồi từ snapshot bắt buộc phải trả về chính xác số dư và nonce tại Block A giống hệt như checkpoint đã lưu ở Bước 3. Quá trình này cũng sẽ tự động chạy thêm 1 vòng kiểm tra gửi giao dịch thực tế rồi lưu lại lịch sử trước khi tiếp tục.


### 5. Gửi giao dịch kiểm tra (Tuần tự)
Sau khi khôi phục và xác minh lịch sử thành công, script thực hiện kiểm tra tính sẵn sàng của node bằng cách:
- Chờ 10 giây cho node ổn định.
- Bắn **tuần tự** 20,000 giao dịch trực tiếp vào chính node vừa khôi phục (`NODE_ID`) sử dụng flag `--target-node $NODE_ID`. Nếu tiến trình này gặp lỗi, script sẽ dừng ngay lập tức.
- Tiếp tục bắn **tuần tự** thêm 20,000 giao dịch qua một node khỏe mạnh khác (`SPAM_NODE`) để đảm bảo mạng lưới vẫn đồng thuận tốt. Việc chạy tuần tự tránh được lỗi đụng độ Nonce (Nonce Collision), trong khi `block_hash_checker` chạy ngầm từ Bước 1 vẫn liên tục giám sát mạng.

### 6. Xác minh đồng thuận Hash & Dọn dẹp tài nguyên
- Chờ 10 giây, sau đó kiểm tra xem tiến trình `block_hash_checker` có phát hiện bất kỳ sự lệch hash block nào giữa các node hay không. Nếu có lệch hash, pipeline sẽ báo lỗi và dừng lập tức.
- Dừng tiến trình `block_hash_checker` của vòng lặp hiện tại, xóa các file tạm và chuẩn bị cho vòng lặp tiếp theo.

---

## 🚀 Hướng dẫn chạy

### Cú pháp:
```bash
cd scripts/
./test-snapshot.sh [options]
```

### Các tùy chọn (Options):
- `--loops <num>`: Số vòng lặp chạy test liên tục (mặc định: 2000). Trong mỗi vòng, script sẽ **tự động luân phiên Node** (0, 1, 2, 3) để thực hiện restore.
- `--tps-rounds <num>`: Số vòng chạy TPS blast trước snapshot (mặc định: 1).
- `--tps-count <num>`: Số lượng giao dịch trong mỗi vòng TPS (mặc định: 20000).

### Ví dụ:
- Chạy test khôi phục snapshot luân phiên các node trong mạng với 5 vòng lặp:
  ```bash
  ./test-snapshot.sh --loops 5
  ```
- Chạy với số lượng TPS cao hơn (50,000 giao dịch mỗi lần):
  ```bash
  ./test-snapshot.sh --loops 3 --tps-count 50000
  ```
