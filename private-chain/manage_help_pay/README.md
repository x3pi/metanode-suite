# Quản lý & Test Hệ Thống Ví Chuyển Tiền Hộ (HelpPay Sponsor Pool)

Tool quản lý các ví bảo trợ (sponsor wallets) chuyển tiền hộ / nạp gas tự động cho người dùng khi số dư không đủ. 
Hệ thống sử dụng cơ chế RPC Node trực tiếp, hỗ trợ nhiều ví chạy song song không nghẽn và **tự động đồng bộ sang tất cả các node trong cụm Raft**.

---

## 1. Cấu hình `config.json`
Đảm bảo file `config.json` cùng thư mục cấu hình đúng địa chỉ RPC của node:
```json
{
  "http_rpc": "http://127.0.0.1:8747",
  "admin_password": "mysecretpassword",
  "admin_private_key": "0x... (Private key của ví Admin được ủy quyền bật/tắt)",
  "chain_id": 991
}
```

> **Cấu hình ví Admin trên Node (`config.json` của node):**
> Trong file `config.json` của simple_chain, bạn có thể cấu hình 1 hoặc nhiều ví admin:
> ```json
> "help_pay": {
>   "enabled": true,
>   "admin_addresses": [
>     "0x550a2b00511883F2042171E9f8C1C73d420E775d"
>   ],
>   "admin_address": "0x550a2b00511883F2042171E9f8C1C73d420E775d",
>   "min_balance": "10000000000000000",
>   "extra_amount": "100000000000000000"
> }
> ```
> *(Hỗ trợ nhiều ví admin để tránh rủi ro mất key, hoặc điền 1 ví đơn lẻ là đủ).*

---

## 2. Hướng dẫn sử dụng

### Bật / Tắt tính năng chuyển tiền hộ (Toggle on / off)
```bash
# Bật tính năng:
go run main.go toggle on
# hoặc:
go run main.go enable

# Tắt tính năng:
go run main.go toggle off
# hoặc:
go run main.go disable
```
- Khi chạy lệnh, tool sẽ tự động dùng `admin_private_key` ký chữ ký ECDSA (chuẩn Ethereum EIP-191) kèm timestamp chống replay.
- Node xác thực người ký nằm trong danh sách admin đã cấu hình, sau đó cập nhật trạng thái và tự động **đồng bộ sang toàn bộ cụm Raft**.

---

### Xem danh sách ví chuyển hộ và cấu hình hiện tại
```bash
go run main.go list
```
Hiển thị danh sách các ví trong pool, số dư on-chain, số lượng giao dịch đã gửi, thời gian dùng cuối cùng, danh sách ví admin, và ngưỡng kích hoạt `min_balance`.
*(Lệnh được tự động xác thực bằng chữ ký ví Admin EIP-191 hoặc mật khẩu `admin_password`)*.

---

### Thêm ví chuyển hộ mới (Tự động đồng bộ Raft)
```bash
go run main.go add <private_key_hex>
```
*Ví dụ:*
```bash
go run main.go add 0x4f3edf983ac636a65a842ce7c78d9aa706d3b113bce9c46f30d7d21715b23b1d
```
> **Lưu ý:**
> - Cần truyền **Private Key** (để node có quyền ký giao dịch nạp gas tự động). Địa chỉ ví sẽ được tự động trích xuất từ private key và đồng bộ sang tất cả các node trong cụm Raft.
> - Bắt buộc phải có quyền Admin (`admin_private_key` hoặc `admin_password` trong `config.json`). Nếu node chưa cấu hình Admin, yêu cầu sẽ bị từ chối (Fail-Closed).

---

### Xóa ví chuyển hộ (Tự động đồng bộ Raft)
```bash
go run main.go remove <địa_chỉ_ví_hoặc_private_key>
```
*Ví dụ:*
```bash
go run main.go remove 0x9065e2caaa4bf6533ded0b2a763e4ee81cd31bd2
```
*(Bắt buộc phải có quyền Admin để xóa ví khỏi pool)*.

---

### Xem cấu hình HelpPay
```bash
go run main.go config
```

---

### Cập nhật cấu hình HelpPay (Bật/tắt, ngưỡng kích hoạt, số tiền nạp)
```bash
go run main.go set-config <enabled: true|false> [min_balance_wei] [extra_amount_wei]
```
*Ví dụ:*
```bash
go run main.go set-config true 10000000000000000 100000000000000000
```

---

### Test tự động quy trình nạp tiền hộ (End-to-End Test)
```bash
go run main.go test-topup
```
Quy trình test tự động:
1. Sinh ngẫu nhiên một ví người dùng mới có số dư bằng 0.
2. Gửi một giao dịch từ ví này lên chain qua `eth_sendRawTransaction`.
3. Hệ thống node phát hiện số dư < `min_balance`, điều phối ví bảo trợ đang rảnh trong pool chuyển tiền hộ `extra_amount`.
4. Giao dịch của người dùng được xác nhận thành công mà không bị reject vì thiếu tiền gas.

---

## 3. Khả năng chịu lỗi & Tự phục hồi của Cụm Node (Fault Tolerance & Auto Recovery)

- **Khi 1 hoặc nhiều node bị chết / gián đoạn:**
  - Các node còn lại trong cụm vẫn tiếp tục xử lý các yêu cầu nạp gas bình thường.
  - Bạn vẫn có thể thêm/xóa ví hoặc bật/tắt cấu hình trên bất kỳ node nào đang sống.
- **Khi node offline sống lại:**
  - **Tự động kéo bù (Startup Catch-up Sync)**: Node vừa khởi động lại sẽ tự động liên hệ các peer đang sống trong cụm qua kênh HTTP nội bộ Raft (`/raft/v1/admin/helppay_sync`) để kéo danh sách ví và cấu hình mới nhất về gộp vào node của mình.
  - **Lưu trữ an toàn (Atomic Persistence)**: File cấu hình ví trên đĩa (`helppay_wallets.json`) được ghi theo cơ chế nguyên tử (Atomic Write qua file tạm và `os.Rename`), chống triệt để rủi ro hỏng file khi mất nguồn điện đột ngột.
