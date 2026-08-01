# Quản lý Ví Trả Hộ (Help Pay Wallet)

Tool dùng để Thêm/Xóa các ví có nhiệm vụ trả phí Gas hoặc phát Reward cho user.

## Cấu hình `config.json`
Đảm bảo file `config.json` nằm cùng thư mục với các thông tin:
```json
{
  "http_rpc": "http://127.0.0.1:8545",
  "eth_private_key": "PRIVATE_KEY_ADMIN",
  "chain_id": 1337,
  "contact_private": "ĐỊA_CHỈ_CONTRACT"
}
```

## Cách dùng

**1. Lấy danh sách ví trả hộ hiện tại**
```bash
go run main.go list
```

**2. Thêm ví trả hộ**
```bash
go run main.go add <địa_chỉ_ví>
```
*Ví dụ:* `go run main.go add 0x5e582475A504998c5631E12A5a2585D2B1911812`

**3. Xóa ví trả hộ**
```bash
go run main.go remove <địa_chỉ_ví>
```
*Ví dụ:* `go run main.go remove 0x123...`

Hệ thống RPC Node sẽ **tự động cập nhật** danh sách ngay lập tức khi giao dịch thành công. Không cần khởi động lại server.
