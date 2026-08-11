# Công cụ TPS Blast (Parallel Smart Contract) 🚀

Thư mục này chứa một bộ công cụ kiểm thử hiệu năng cao (Load Test / Spam Tool) được tinh chỉnh ĐẶC BIỆT để ép Node của Metanode phải chạy ở chế độ **Thực thi Song Song (True Block-STM)**. Bằng cách rải đều giao dịch ra hàng loạt các Contract (Multiple Contracts), bộ test này giúp vượt qua giới hạn bảo vệ tự động của Metanode và vắt kiệt sức mạnh đa luồng của hệ thống.

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
- `-count`: Tổng số lượng giao dịch muốn bắn (Mặc định: `10000`).
- `-batch`: Kích thước mỗi cụm giao dịch (batch) đẩy xuống TCP (Mặc định: `500`).
- `-rounds`: Số vòng lặp chạy test. Chạy xong đợt 1 chờ confirm rồi chạy tiếp đợt 2 (Mặc định: `1`).
- `-verify`: Bật chế độ đợi lấy Receipt từ RPC sau khi gửi xong, rất tốn thời gian nhưng chắc chắn (Mặc định: `false`).
- `-conflict`: Chế độ cố tình ghi đè biến dùng chung để test cơ chế phát hiện xung đột của BlockSTM (Mặc định: `false`).
- `-num-contracts`: Số lượng Smart Contract sẽ được rải ra mạng (Mặc định: `1000`). Rải càng nhiều, tỷ lệ đụng độ càng thấp, hệ thống càng chạy song song mượt mà.
- `-load_balance`: Bật phân bổ chia đều giao dịch ra nhiều node thay vì dồn vào 1 node (`-load_balance=true`).
- `-trace`: Bật tính năng log chi tiết để xem giao dịch được đưa vào Block nào (`-trace=true`). (Mặc định: `false`)

GO_FLAGS="-config=config.json -count=10000 -rounds=1000000 -num-contracts=10000 -load_balance -check-state"
## 3. Các Lệnh Chạy Mẫu (Examples)
**Chạy test mặc định (10.000 txs, batch 500, 1 node):**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=10000
```

**Chạy test tải nặng (50.000 txs) ép hệ thống chạy song song qua 1000 hợp đồng:**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=50000 -batch=10000 -load_balance=true -num-contracts=1000
```

**Chạy bài test độ bền (Endurance Test) với 5 vòng (rounds) liên tiếp, mỗi vòng 20,000 txs:**
```bash
go run main.go -config=./config-multi.json -keys=../gen_spam_keys/generated_keys.json -count=20000 -rounds=5 -verify=true
```

## 4. Hoạt động của Script
Khi bạn chạy lệnh trên, script sẽ:
1. Tự động dùng `$N` tài khoản đầu tiên để **Deploy `$N` Smart Contracts** (`ParallelTest.sol`) lên mạng thông qua RPC. Việc này diễn ra hoàn toàn đa luồng cực kỳ nhanh.
2. Build song song 10.000 giao dịch (hoặc bằng số `-count` bạn truyền vào). Các giao dịch này sẽ gọi hàm `updateState(1)` và được **rải đều ngẫu nhiên (round-robin)** vào 1 trong số `$N` contracts vừa tạo.
3. Nhóm các giao dịch thành từng cụm (dựa theo `-batch`).
4. Bắn toàn bộ giao dịch vào mạng thông qua TCP (cổng 6200/6201) với tốc độ siêu cao.
5. Ngồi chờ và tự động quét Block để báo cáo xem có bao nhiêu giao dịch đã thực sự được commit thành công vào blockchain trong 1 Epoch.

