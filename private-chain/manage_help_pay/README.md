# Quản lý Ví Trả Hộ (Help Pay Wallet)

Tool dùng để Thêm/Xóa các ví có nhiệm vụ trả phí Gas hoặc phát Reward cho user.

## Cấu hình `config.json`
Đảm bảo file `config.json` nằm cùng thư mục với các thông tin:
```json
{
  "http_rpc": "192.168.1.234:8545",
  "eth_private_key": "PRIVATE_KEY_ADMIN",
  "chain_id": 991,
  "contact_private": "0x00000000000000000000000000000000D844bb55"
}
```

## Cách dùng

**1. Lấy danh sách ví trả hộ hiện tại**
```bash
go run main.go list
```

**2. Thêm ví trả hộ (dùng Private Key)**
```bash
go run main.go add <private_key>
```
*Ví dụ:* `go run main.go add 48d0959736be4999d4fcfce48423e2e0f98610663ae3ee87f879eb69f74a0746`  
*(Tool tự động tính ra địa chỉ ví tương ứng và lưu private key vào node để ký giao dịch).*

**3. Xóa ví trả hộ**
```bash
go run main.go remove <địa_chỉ_ví_hoặc_private_key>
```
*Ví dụ theo địa chỉ ví:* `go run main.go remove 0x0B143e894a600114C4A3729874214e5fC5EA9cbc`  
*Ví dụ theo private key:* `go run main.go remove 48d0959736be4999d4fcfce48423e2e0f98610663ae3ee87f879eb69f74a0746`

Hệ thống RPC Node sẽ **tự động cập nhật** danh sách ngay lập tức khi giao dịch thành công. Không cần khởi động lại server.
