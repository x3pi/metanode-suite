# Check Parent Hashes Tool

Công cụ dùng để trace (truy vết) ngược danh sách các block liên tiếp trên một node để kiểm tra chuỗi `Hash` và `ParentHash` có liền mạch hay không. Công cụ đặc biệt hữu ích khi cần debug lỗi phân nhánh (fork), missing block, hay xác định block nào bắt đầu rẽ nhánh.

## Cú pháp

```bash
go run check_parent_hashes.go <nodeURL> <startBlockNum> <count> [targetHash]
```

### Tham số
1. **`nodeURL`**: Địa chỉ RPC của node (VD: `http://127.0.0.1:8757`).
2. **`startBlockNum`**: Block bắt đầu để trace ngược về trước.
3. **`count`**: Số lượng block muốn lấy.
4. **`targetHash`** *(Tùy chọn)*: Nếu truyền vào một mã hash, tool sẽ báo động (`<=== 🚨 MATCHED HASH!`) nếu phát hiện `Hash` hoặc `ParentHash` của block đó khớp với target này.

## Ví dụ sử dụng

```bash
go run check_parent_hashes.go http://127.0.0.1:8757 6516 100 0x84ecd2fdcb83c60d83291a65ccef83e4e0dc311591b783f09a0d2d175eb84a7c
```
*Lệnh trên sẽ kết nối tới node local port 8757, trace ngược 100 block tính từ block 6516 và tìm kiếm sự xuất hiện của hash `0x84ec...`.*
