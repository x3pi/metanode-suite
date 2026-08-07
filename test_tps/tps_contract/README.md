# TPS Contract Load Tester

Công cụ này dùng để bắn hàng ngàn (hoặc hàng chục ngàn) giao dịch gọi Smart Contract (ghi trạng thái) thông qua TCP một cách song song để kiểm tra khả năng chịu tải và Block-STM của Metanode.

## 1. Biên dịch (Chỉ cần chạy 1 lần nếu có thay đổi code)
Trước khi chạy, bạn cần biên dịch mã nguồn Go ra file thực thi:
```bash
go build -o tps_contract main.go
```

## 2. Cách chạy Test
Sử dụng lệnh sau để chạy bài test.
Lưu ý: 
- Bạn có thể trỏ `config` tới `config.json` hoặc `config-multi.json` trong thư mục hiện tại.
- File `keys` thường lấy từ thư mục `gen_spam_keys`.
- Tham số `-count` quyết định số lượng giao dịch muốn bắn ra (Ví dụ: `10000`).

```bash
./tps_contract -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=10000
```

### Các thông số tùy chọn quan trọng:
- `-config`: Đường dẫn tới file chứa danh sách IP/Port của các node. (Mặc định: `./config.json`)
- `-keys`: Đường dẫn tới file chứa danh sách Private Key để sign giao dịch. (Bắt buộc)
- `-count`: Số lượng giao dịch bạn muốn gửi. (Ví dụ: 10000)
- `-batch`: Số lượng giao dịch đóng gói vào mỗi cục (batch) trước khi gửi qua TCP. (Mặc định: 500)
- `-rounds`: Số vòng lặp test muốn chạy liên tiếp. (Mặc định: 1)
- `-verify`: Bật kiểm tra đối chiếu Balance và Receipt sau khi chạy xong (`-verify=true`). (Mặc định: `false`)
- `-load_balance`: Bật phân bổ chia đều giao dịch ra nhiều node thay vì dồn vào 1 node (`-load_balance=true`).
- `-trace`: Bật tính năng log chi tiết để xem giao dịch được đưa vào Block nào (`-trace=true`). (Mặc định: `false`)

## 3. Các Lệnh Chạy Mẫu (Examples)

**Chạy test mặc định (10.000 txs, batch 500, 1 node):**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=10000
```

**Chạy test tải nặng (100.000 txs) chia batch lớn (1000 tx/batch) và chia đều tải ra nhiều node:**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=50000 -batch=10000 -load_balance=true
```

**Chạy bài test độ bền (Endurance Test) với 5 vòng (rounds) liên tiếp, bật verify chéo receipt:**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=20000 -rounds=5 -verify=true
```

go run main.go --config config-multi.json --count 50000 --batch 2000 -rounds=1 --conflict=false -load_balance=true

## 4. Hoạt động của Script
Khi bạn chạy lệnh trên, script sẽ:
1. Dùng Account 0 để Deploy Smart Contract tên là `ParallelTest.sol` lên mạng thông qua RPC.
2. Build song song 10.000 giao dịch (hoặc bằng số `-count` bạn truyền vào) gọi hàm `updateState(1)` trên Contract đó.
3. Nhóm các giao dịch thành từng cụm (dựa theo `-batch`).
4. Bắn toàn bộ giao dịch vào mạng thông qua TCP (cổng 6200/6201) với tốc độ siêu cao (vài trăm ngàn TX/s).
5. Ngồi chờ và tự động quét Block để báo cáo xem có bao nhiêu giao dịch đã thực sự được commit thành công vào blockchain trong 1 Epoch.
