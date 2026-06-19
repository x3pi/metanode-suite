# Register BLS Tool

Công cụ này dùng để đăng ký public key BLS cho ví trên Metanode thông qua giao thức TCP hoặc HTTP RPC. Hỗ trợ hai chế độ chính:
1. **Tạo ví mới** và đăng ký BLS (hỗ trợ tạo hàng loạt).
2. **Đăng ký BLS cho ví có sẵn** bằng Private Key.

---

## Danh sách tham số (Flags)

| Cờ (Flag) | Mô tả | Mặc định |
| :--- | :--- | :--- |
| `-mode` | Giao thức sử dụng (`tcp` hoặc `http`). | `tcp` |
| `-config` | Đường dẫn tới file cấu hình JSON. | `config-test.json` |
| `-count` | Số lượng ví mới cần tạo và đăng ký. (Chỉ áp dụng khi không dùng `-wallet-pk`). | `1` |
| `-out` | Tên file JSON để lưu thông tin các ví vừa tạo. | `bls_keys.json` |
| `-wallet-pk` | Private key (hex) của ví đã có. Nếu có, tool sẽ đăng ký cho ví này thay vì tạo mới. | `""` |
| `-admin-pk` | Private key (hex) của Admin dùng để duyệt. Nếu không điền, dùng `eth_private_key` trong config. | `""` |
| `-no-confirm` | Bỏ qua bước duyệt (không gọi hàm `confirmAccountWithoutSign`). | `false` |
| `-http-rpc` | URL của endpoint HTTP RPC (ví dụ: `http://192.168.1.234:8650`). Chỉ dùng khi `-mode=http`. Nếu không có, sẽ lấy từ trường `"http_rpc"` trong file config. | `""` |

---

## Các Ví dụ Cụ thể

### 1. Tạo ví mới và Đăng ký BLS

Tạo 1 ví mới, đăng ký qua **TCP** (mặc định):
```bash
go run . -count=1
```

Tạo 5 ví mới, đăng ký qua **HTTP**:
```bash
go run . -mode=http -count=5
```

### 2. Đăng ký BLS cho ví CÓ SẴN

**Qua TCP, tự động confirm bằng Admin trong config:**
```bash
go run . -config=config-test.json -wallet-pk=e6b400585f8e1df3bca8302b7657249046d40c8ed92dba9a057287e4beca587a
```

**Qua HTTP, chỉ định URL trực tiếp (hoặc dùng trong config):**
```bash
go run . -mode=http -http-rpc="http://192.168.1.234:6200" -wallet-pk=<PRIVATE_KEY_VI>
```

**Chỉ định Admin Key riêng để duyệt (TCP):**
```bash
go run . -config=config-test.json \
  -wallet-pk=<PRIVATE_KEY_VI> \
  -admin-pk=<PRIVATE_KEY_ADMIN>
```

**Không cần confirm (Bỏ qua confirmAccountWithoutSign):**
```bash
go run . -config=config-test.json \
  -wallet-pk=<PRIVATE_KEY_VI> \
  -no-confirm
```

### 3. Khởi tạo dữ liệu hàng loạt cho TPS Blast
Để tạo hàng chục ngàn tài khoản để test TPS (dùng tool `tps_blast_cc`), tool hỗ trợ mode HTTP chạy cực nhanh:
- **KHÔNG** bị giới hạn độ trễ (`time.Sleep`).
- **KHÔNG** chờ receipt trả về mà chỉ lấy `txHash` để đi tiếp.
- Định dạng file đầu ra chuẩn có sẵn `index`, `private_key`, `address` tương thích 100% với `tps_blast_cc`.

**Ví dụ lệnh:** Tạo 40,000 account và lưu thẳng vào thư mục của `tps_blast_cc`:
```bash
go run . -mode=http -count=40000 -out=../test_tps/gen_spam_keys/generated_keys.json
```
*(Với mỗi account, tool sẽ lấy 1 BLS public key riêng biệt từ node, đăng ký và duyệt cực nhanh)*
