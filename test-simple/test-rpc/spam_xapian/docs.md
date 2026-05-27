# 🚀 Xapian Spam Tool

Công cụ này dùng để tạo áp lực (Stress Test / Spam) lên mạng lưới bằng cách bắn hàng nghìn giao dịch liên tục vào Contract Xapian, giúp kiểm tra tính ổn định, TPS (Transactions Per Second) và khả năng xử lý đồng thời (Thread-safe) của Node.

## 🎯 Tính năng cốt lõi
- **Tự động Deploy:** Đọc mã Bytecode từ file cấu hình JSON và tự động deploy Contract trước khi spam.
- **Auto Nonce Tracking:** Tự động theo dõi số Nonce cục bộ trên RAM thay vì spam RPC Node, giúp tránh nghẽn Node (Lỗi Invalid Nonce). Tự động phục hồi Nonce nếu mạng rớt.
- **Realtime Log:** Hiển thị log trực tiếp từng Transaction được gửi đi và trạng thái thành công/thất bại (Revert) của nó.
- **Zero Sleep (Khát nước):** Bắn tối đa công suất. Hết 1 vòng là lập tức gửi tiếp vòng sau, không chèn thời gian nghỉ.

---

## 🏃‍♂️ Cách sử dụng (Quick Start)

Mở Terminal trong thư mục chứa file `run_spam.sh` (`test-simple/test-rpc/spam_xapian/`) và chạy một trong hai cách sau:

### Cách 1: Tự động Deploy và Spam (Khuyên dùng)
Nếu bạn chưa có Contract, hoặc muốn test trên một môi trường hoàn toàn mới. Công cụ sẽ tự Deploy Contract rồi Spam luôn.
```bash
./run_spam.sh deploy [TênHàm] [SốLượngVí]
```
**Ví dụ:** Deploy và dùng 1000 ví bắn vào hàm `runStep1_Setup`
```bash
./run_spam.sh deploy runStep1_Setup 1000
```

### Cách 2: Spam vào Contract đã có sẵn
Nếu bạn đã biết địa chỉ Contract và chỉ muốn đẩy giao dịch vào đó.
```bash
./run_spam.sh <ĐịaChỉContract> [TênHàm] [SốLượngVí]
```
**Ví dụ:**
```bash
./run_spam.sh 0xBad2298cBD00E7b385AcF4B68e66e892843B9953 runStep3_UpdateDoc 500
```

---

## ⚙️ Cơ chế hoạt động (Dành cho Developer)
1. **Lấy danh sách ví:** Đọc từ file `../../../test_tps/gen_spam_keys/generated_keys.json` (1000 private keys).
2. **Khởi tạo Nonce:** Ping RPC Node duy nhất 1 lần để lấy số Nonce ban đầu cho mỗi ví (Cơ chế `fetchNonceWithRetry` - Nếu Node bị trễ Mempool, nó tự đợi 500ms để thử lại 5 lần).
3. **Gửi Giao Dịch:** Dùng vòng lặp Goroutines tạo ra HÀNG NGHÌN luồng chạy song song, mỗi luồng đại diện cho 1 ví thực hiện việc gửi Tx. Ngay khi Tx được gửi, Nonce của ví đó tăng lên `+1`.
4. **Lắng nghe (Wait Receipt):** Chặn (Block) luồng Goroutine để kiểm tra tình trạng Tx. Timeout là `30s`, khoảng thời gian poll là `100ms`. Nếu Tx thành công / thất bại, nó báo cáo ra màn hình và kết thúc luồng.
5. **Round mới:** Ngay khi toàn bộ 1000 Goroutine hoàn tất, vòng lặp to ở ngoài ngay lập tức quăng mẻ 1000 giao dịch tiếp theo lên Mempool. Bão tố không bao giờ ngừng lại cho đến khi bạn nhấn `Ctrl + C`.
