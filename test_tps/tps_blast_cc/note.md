# Tài liệu hướng dẫn sử dụng công cụ TPS Blast Cross-Chain (`tps_blast_cc`)

## 🛠 Danh sách các tham số (Flags)

| Tham số | Giá trị mặc định | Mô tả |
| :--- | :--- | :--- |
| `--config` | `./config.json` | Đường dẫn đến file cấu hình JSON của Client chứa thông tin private key và connection address. |
| `--keys` | `../gen_spam_keys/generated_keys.json` | Đường dẫn tới file JSON chứa danh sách các khóa bảo mật được sinh ra để chạy spam TXs. |
| `--count` | `10000` | Tổng số lượng giao dịch muốn tạo ra để gửi lên mạng lưới (lockAndBridge hoặc Native). |
| `--batch` | `500` | Số lượng giao dịch tối đa được gộp chung và gửi đi trong mỗi batch thông qua TCP. |
| `--sleep` | `10` | Thời gian nghỉ (sleep) tính bằng mili-giây (ms) giữa các đợt bắn batch giao dịch. |
| `--node` | `""` | Ghi đè địa chỉ TCP của node. Có thể truyền nhiều node phân tách bằng dấu phẩy (vd: `node1:port,node2:port`). |
| `--rpc` | `""` | Địa chỉ HTTP RPC dùng để kiểm tra thông tin tài khoản, số dư hoặc xác nhận (VD: `http://localhost:8757`). |
| `--wait` | `120` | Thời gian chờ tối đa (giây) để các giao dịch được đóng gói thành công vào block trước khi báo lỗi Timeout. |
| `--recipient` | `0xbF2b4B9b9dFB6...` | Địa chỉ ví nhận tiền trên chuỗi đích (destination chain). |
| `--dest` | `2` | ID của chuỗi đích (Destination Chain ID) để thực hiện giao dịch cross-chain. |
| `--amount` | `100` | Số lượng coin giao dịch tính bằng Wei (mặc định là 100 Wei). |
| `--rounds` | `1` | Số vòng (rounds) chạy kiểm tra hiệu năng liên tiếp. |
| `--load_balance` | `false` | Nếu bật `true`, giao dịch sẽ được chia đều xoay vòng (round-robin) qua tất cả các `connection_node_*` có trong cấu hình. |
| `--verify` | `false` | Xác minh số dư tài khoản nhận tiền sau khi hoàn thành mỗi vòng để đảm bảo giao dịch thực tế đã thành công. |
| `--epoch-wait` | `600` | Thời gian tối đa (giây) chờ cho hệ thống chuyển dịch sang Epoch mới trước khi bắt đầu tính timeout giao dịch. Gán `0` để tắt chức năng này. |
| `--target-node` | `0` | Chỉ định ID của node đích (từ `0` đến `3`) để gửi giao dịch. Công cụ tự động cấu hình TCP & RPC tương ứng từ `config.json`. |
| `--trace` | `false` | Nếu bật `true`, công cụ sẽ tự động gọi RPC để lấy block traces (thông tin thời gian thực thi nội bộ) sau khi kết thúc mỗi vòng và in ra báo cáo. |

## ⚙️ Cơ chế định tuyến tự động & xử lý lỗi nghiêm ngặt (Strict Error Handling)

Từ phiên bản nâng cấp, công cụ hỗ trợ cơ chế định tuyến và kiểm soát lỗi toàn diện phục vụ kiểm thử khôi phục hệ thống (Recovery & Snapshot Tests):
- **Tự động cấu hình theo Node ID**: Khi truyền flag `--target-node <ID>`, công cụ sẽ đọc `config.json` để tự động chọn đúng cổng TCP (`connection_node_<ID>`) và cổng HTTP RPC (`rpc_<ID>`). Đối với Node 0, nó sử dụng `parent_connection_address` và `rpc_0`.
- **Dừng ngay lập tức khi gặp lỗi (Crash-on-Error)**: Khi bất kỳ bước nào trong quy trình gửi giao dịch gặp sự cố, tiến trình sẽ **thoát ngay lập tức với mã lỗi 1** kèm thông tin chẩn đoán chi tiết:
  - *Lỗi lấy nonce*: In rõ danh sách địa chỉ RPC và thoát, không gửi giao dịch lỗi.
  - *Lỗi kết nối TCP*: Nếu node bị tắt hoặc hỏng, tiến trình thoát ngay sau 30 lần thử kết nối lại thất bại.
  - *Lỗi ghi dữ liệu (Write error)*: Nếu mất kết nối khi đang gửi batch, tiến trình sẽ thử reconnect đúng 1 lần; nếu vẫn lỗi, nó sẽ dừng chương trình lập tức thay vì bỏ qua như trước.
  - *Lỗi xác nhận khối (Timeout)*: Nếu hết thời gian chờ mà giao dịch chưa được đóng gói hoàn toàn, tiến trình báo lỗi cụ thể kèm danh sách node RPC/TCP đang kết nối.

## 💡 Ví dụ chạy lệnh (Examples)

### 1. Chỉ định bắn giao dịch vào Node 2:
```bash
go run main.go --count 5000 --target-node 2

go run main.go --count 20000 --rounds 30 --load_balance=false --batch=10 --amount 1 --config=config-multi.json --trace

go run main.go --count 20000 --rounds 20 --load_balance=false --batch=300 --amount 1 --config=config-multi.json --trace

go run main.go --count 20000 --rounds 1 --load_balance=false --batch=300 --amount 1
```

### 2. Chạy tải song song với cơ chế Epoch Wait (Mặc định 10 phút / 600 giây):
```bash
go run main.go --count 10000 --epoch-wait 600 --target-node 0
```

### 3. Tắt cơ chế chờ chuyển epoch (Bắn TX và tính giờ timeout luôn):
```bash
go run main.go --count 20000 --epoch-wait 0 --batch 500 --target-node 1
```

### 4. Chạy tải nặng song song (Parallel Native) chia đều qua nhiều node (Load Balancing):
```bash
go run main.go --count 50000 --rounds 5 --load_balance=true --batch 10000 --sleep 0 --epoch-wait 300
```

### 5. Kết hợp trong script Node Recovery / Snapshot:
Trong quá trình node đang dừng hoặc vừa khôi phục, chúng ta định tuyến tải tới các node cụ thể:
```bash
# Gửi giao dịch ngầm tạo GAP lên Node 1 khi Node 0 đang tắt
go run main.go --count 20000 --target-node 1 > blast_gap.log 2>&1 &

# Kiểm tra sức chịu tải cụ thể trên Node vừa khôi phục (Ví dụ Node 2)
go run main.go --count 5000 --target-node 2 > blast_restore.log 2>&1
```

## 🚀 Kịch bản tự động hóa toàn bộ quy trình (`run_tps_test.sh`)

Công cụ hỗ trợ kịch bản tự động chạy toàn bộ quy trình sinh khóa chuẩn, cấu hình genesis, triển khai và khởi động lại cụm validator, sau đó kích hoạt bài test hiệu năng thông qua tệp [run_tps_test.sh](file:///home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/run_tps_test.sh).

### Các tùy chọn nâng cao:
- `--no-reset`: Bỏ qua quá trình sinh khóa mới và deploy reset lại cụm node (giữ nguyên cơ sở dữ liệu blockchain và ví hiện có, chỉ kích hoạt chạy benchmark).
- Số lượng ví và các tùy chọn khác (`--rounds`, `--batch`, `--load_balance`, ...) có thể truyền trực tiếp từ dòng lệnh.

### Ví dụ sử dụng:

1. **Sinh mới khóa chuẩn và reset cụm node chạy lại từ đầu:**
   ```bash
   ./run_tps_test.sh 50000 --rounds 3 --batch 20000
   ```

2. **Chỉ chạy test TPS (không dọn dẹp database, giữ nguyên ví cũ):**
   ```bash
   ./run_tps_test.sh --no-reset 20000 --rounds 5 --load_balance false
   ```
