# 🚀 Xapian Spam Tool

Công cụ này dùng để tạo áp lực (Stress Test / Spam) lên mạng lưới bằng cách bắn hàng nghìn giao dịch liên tục vào Contract Xapian, giúp kiểm tra tính ổn định, TPS (Transactions Per Second) và khả năng xử lý đồng thời (Thread-safe) của Node.

## 🎯 Tính năng cốt lõi
- **Tự động Deploy:** Đọc mã Bytecode từ file cấu hình JSON và tự động deploy Contract trước khi spam.
- **Auto Nonce Tracking:** Tự động theo dõi số Nonce cục bộ trên RAM thay vì spam RPC Node, giúp tránh nghẽn Node (Lỗi Invalid Nonce). Tự động phục hồi Nonce nếu mạng rớt.
- **Realtime Log:** Hiển thị log trực tiếp từng Transaction được gửi đi và trạng thái thành công/thất bại (Revert) của nó.
- **Multi-Contracts (Round-robin):** Hỗ trợ tính năng tự động deploy nhiều contract (Ví dụ: 10 contract) cùng lúc và phân tán lưu lượng bằng cách gán xoay vòng các contract này cho N ví (Ví dụ: 10 contract cho 1000 ví). Giúp giảm nghẽn trạng thái (state IO bottleneck) trên mạng lưới.
- **Zero Sleep (Khát nước):** Bắn tối đa công suất. Hết 1 vòng là lập tức gửi tiếp vòng sau, không chèn thời gian nghỉ.

---

## 🏃‍♂️ Cách sử dụng (Quick Start)

Mở Terminal trong thư mục `test-simple/test-rpc/spam_xapian/` và chạy trực tiếp bằng lệnh `go run main.go`.

### 📌 Các tham số (Flags) quan trọng:
- `-config`: Đường dẫn tới file config JSON (Mặc định: `../config-local.json`).
- `-keys`: Đường dẫn tới file chứa danh sách private keys (Mặc định: `../../../test_tps/gen_spam_keys/generated_keys.json`).
- `-wallets`: Số lượng ví sẽ dùng để spam (Mặc định: `1000`).
- `-rounds`: Số vòng spam tối đa (Mặc định: `2000`).
- `-method`: Tên hàm trong contract để gọi (Ví dụ: `runStep1_Setup`, `runStep3_UpdateDoc`).
- `-deploy-json`: (Chỉ dùng khi tự động deploy) File JSON chứa bytecode để deploy (Ví dụ: `deploy.json`).
- `-num-contracts`: Số lượng contract muốn tự động deploy và chia đều xoay vòng (round-robin) cho các ví (Mặc định: `1`).
- `-contract`: Dùng khi đã có sẵn địa chỉ contract, không tự deploy nữa. Có thể truyền 1 hoặc danh sách nhiều địa chỉ cách nhau bằng dấu phẩy.

---

### Cách 1: Tự động Deploy nhiều Contract và Spam (Khuyên dùng)
Tính năng ưu việt giúp tự Deploy **N Contracts** rồi gán xoay vòng cho **M ví** trước khi spam. Tăng cường phân tán tải trên mạng.
**Ví dụ:** Tự động deploy **10 contract** và dùng **1000 ví** bắn vào hàm `runStep1_Setup`:
```bash
go run main.go -wallets=2000 -deploy-json=deploy.json -num-contracts=200 -method=runStep1_Setup
```

### Cách 2: Tự động Deploy 1 Contract duy nhất
Nếu bạn muốn tạo 1 contract duy nhất và cho toàn bộ 1000 ví cùng gọi lên đó (gây áp lực IO state lớn cho 1 account).
**Ví dụ:**
```bash
go run main.go -wallets=1000 -deploy-json=deploy.json -method=runStep1_Setup
```

### Cách 3: Spam vào Contract đã có sẵn
Chỉ áp dụng nếu bạn đã biết địa chỉ Contract từ trước.
**Ví dụ 1:** Dùng 500 ví gọi hàm `runStep3_UpdateDoc` trên **1 contract** có sẵn:
```bash
go run main.go -wallets=500 -contract="0xBad2298cBD00E7b385AcF4B68e66e892843B9953" -method=runStep3_UpdateDoc
```
**Ví dụ 2:** Truyền **nhiều contract thủ công** cách nhau bằng dấu phẩy (Code tự động chia round-robin cho ví):
```bash
go run main.go -wallets=500 -contract="0x111...,0x222...,0x333..." -method=runStep3_UpdateDoc
```

---

## ⚙️ Cơ chế hoạt động (Dành cho Developer)
1. **Lấy danh sách ví:** Đọc từ file `../../../test_tps/gen_spam_keys/generated_keys.json` (1000 private keys).
2. **Khởi tạo Nonce:** Ping RPC Node duy nhất 1 lần để lấy số Nonce ban đầu cho mỗi ví (Cơ chế `fetchNonceWithRetry` - Nếu Node bị trễ Mempool, nó tự đợi 500ms để thử lại 5 lần).
3. **Gửi Giao Dịch:** Dùng vòng lặp Goroutines tạo ra HÀNG NGHÌN luồng chạy song song, mỗi luồng đại diện cho 1 ví thực hiện việc gửi Tx. Ngay khi Tx được gửi, Nonce của ví đó tăng lên `+1`.
4. **Lắng nghe (Wait Receipt):** Chặn (Block) luồng Goroutine để kiểm tra tình trạng Tx. Timeout là `30s`, khoảng thời gian poll là `100ms`. Nếu Tx thành công / thất bại, nó báo cáo ra màn hình và kết thúc luồng.
5. **Round mới:** Ngay khi toàn bộ 1000 Goroutine hoàn tất, vòng lặp to ở ngoài ngay lập tức quăng mẻ 1000 giao dịch tiếp theo lên Mempool. Bão tố không bao giờ ngừng lại cho đến khi bạn nhấn `Ctrl + C`.
