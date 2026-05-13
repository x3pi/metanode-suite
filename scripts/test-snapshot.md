# Test Snapshot Pipeline

Công cụ này dùng để test tự động luồng chạy TPS, check hash và khôi phục snapshot cho Metanode.

## Các bước tự động trong script

1. Khởi động `block_hash_checker` chạy ngầm để giám sát lệch hash liên tục giữa các node.
2. Chạy `tps_blast_cc` để spam transaction (mặc định 1 round với 20000 txs).
3. Chờ 10s để cho hệ thống lưu state và block ổn định.
4. Chạy `restore_node.sh` với một Node chỉ định (mặc định Node 1) để test tính năng khôi phục từ snapshot local.
5. Chạy `rpc-tcp-simple.sh` tạo giao dịch mới ngay sau khi node khôi phục thành công.
6. Chờ thêm 10s xem `block_hash_checker` có phát hiện lệch hash không.
7. Lặp lại tiến trình (nếu cấu hình loop > 1).

## File Script

File bash thực thi nằm ở: `scripts/test-snapshot.sh`

## Cách sử dụng

```bash
cd scripts/
./test-snapshot.sh [options]
```

### Các tùy chọn (Options)

- `--node <id>`: Chọn node để restore (mặc định: 1)
- `--loops <num>`: Số vòng lặp tiến trình test (mặc định: 1)
- `--tps-rounds <num>`: Số round chạy TPS blast (mặc định: 1)
- `--tps-count <num>`: Số lượng giao dịch mỗi round (mặc định: 20000)
- `-h, --help`: Hiển thị trợ giúp

### Ví dụ

Chạy test khôi phục snapshot cho node 1, lặp lại 5 lần, mỗi lần spam 10 round TPS:

```bash
./test-snapshot.sh --node 1 --loops 5 --tps-rounds 10
```
