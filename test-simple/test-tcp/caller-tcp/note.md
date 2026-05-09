``` bash
go run main.go -config=config-server.json -data=data.json
go run main.go -config=config-nhat.json -data=transfer.json
go run main.go -config=config-nhat.json -data=data.json


```

``` bash
go run main-no-none.go -config=config-local.json -data=data.json
go run main-no-none.go -config=config-local.json -data=data.json -loop
```

## Tính năng xác thực kết quả (Verify)
Thêm trường `"verify": [...]` vào task có `action: "call"` (hoặc `"read"`) trong file `data.json` để tự động so sánh giá trị trả về. Nếu kết quả không khớp, tool sẽ báo lỗi và dừng ngay lập tức.

**Ví dụ:**
```json
{
  "abi_path": "../../abi/normal-test.json",
  "action": "call",
  "method": "getValue",
  "args": [],
  "verify": [1234]
}
```

## Các Flags hỗ trợ (Command-line arguments)
Bạn có thể tinh chỉnh hành vi của tool thông qua các tham số truyền vào khi chạy lệnh `go run`:

| Flag | Mặc định | Mô tả |
| :--- | :--- | :--- |
| `-config` | `config-main.json` | Đường dẫn đến file cấu hình kết nối TCP (ví dụ: `-config=config-local.json`). |
| `-data` | `data.json` | Đường dẫn đến file định nghĩa các kịch bản/giao dịch cần chạy. |
| `-conn` | *(rỗng)* | Ghi đè (override) địa chỉ IP/Port của MetaNode để kết nối (ví dụ: `-conn=127.0.0.1:4201`). |
| `-pk` | *(rỗng)* | Ghi đè Private Key dùng để ký giao dịch (không cần sửa file JSON). |
| `-chain` | `0` | Ghi đè tham số Chain ID của mạng lưới (ví dụ: `-chain=991`). |
| `-loop` | `false` | Bật chế độ gửi giao dịch liên tục. Tool sẽ lặp lại danh sách giao dịch trong file data vô hạn cho đến khi bạn nhấn `Ctrl+C` (ví dụ: `-loop` hoặc `-loop=true`). Cực kỳ hữu ích để stress-test hoặc spam giao dịch. |

**Ví dụ chạy kết hợp nhiều flag:**
```bash
go run main-no-none.go -config=config-local.json -data=data.json -loop -chain=991
```
