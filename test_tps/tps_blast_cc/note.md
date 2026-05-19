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
| `--parallel_native` | `false` | Nếu bật `true`, công cụ chuyển sang chế độ tự chuyển tiền song song (Native Transfers) thay vì cross-chain contract. |
| `--load_balance` | `false` | Nếu bật `true`, giao dịch sẽ được chia đều xoay vòng (round-robin) qua tất cả các `connection_node_*` có trong cấu hình. |
| `--verify` | `false` | Xác minh số dư tài khoản nhận tiền sau khi hoàn thành mỗi vòng để đảm bảo giao dịch thực tế đã thành công. |
| `--epoch-wait` | `600` | Thời gian tối đa (giây) chờ cho hệ thống chuyển dịch sang Epoch mới trước khi bắt đầu tính timeout giao dịch. Gán `0` để tắt chức năng này. |


## 💡 Ví dụ chạy lệnh (Examples)

### 1. Chạy cơ bản với Epoch Wait (Mặc định 10 phút / 600 giây):
```bash
go run main.go --count 10000 --epoch-wait 600
```

### 2. Tắt cơ chế chờ chuyển epoch (Bắn TX và tính giờ timeout luôn không cần đợi epoch):
```bash
go run main.go --count 20000 --parallel_native=true --epoch-wait 0 --batch 500
```

### 3. Cấu hình thời gian chờ epoch ngắn hơn (Ví dụ: Chờ tối đa 30 giây):
```bash
go run main.go --count 5000 --parallel_native=true --epoch-wait 30 --rounds 3 --batch 100 --verify
```

### 4. Các kịch bản chạy benchmark khác:
```bash
# Chạy single node kiểm tra kết quả giao dịch
go run main.go --count 1000 --parallel_native=true --rounds 1 --load_balance=false --batch 5 --verify --epoch-wait 120

# Chạy tải nặng song song (Parallel Native) chia đều qua nhiều node
go run main.go --count 50000 --parallel_native=true --rounds 5 --load_balance=true --batch 10000 --sleep 0 --epoch-wait 300
```

``` bash
./generate_reports.sh 0 1 2 3
```

# get logs

grep -rn "\[MVM CLEANUP\]" /home/abc/nhat/con-chain-v2/mtn-consensus/metanode/logs/node_0

grep -rn " Hoàn thành đồng bộ (Lưu" /home/abc/nhat/consensus-chain/mtn-consensus/metanode/logs/node_1

# Check master (phải trả nonce cao)

curl -s <http://192.168.1.234:8646> -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x4474E7E565E684bE0f054322431F5273817e696A","latest"],"id":1}'

# Check sub-node 233 (đang trả nonce=1, PHẢI bằng master)

curl -s <http://192.168.1.233:10650> -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x4474E7E565E684bE0f054322431F5273817e696A","latest"],"id":1}'

# Check sub-node 231

curl -s <http://192.168.1.234:10747> -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_getTransactionCount","params":["0x4474E7E565E684bE0f054322431F5273817e696A","latest"],"id":1}'


╔═══════════════════════════════════════════════════╗
║  📊 BENCHMARK SUMMARY
╠═══════════════════════════════════════════════════╣
║  🔄 Rounds         : 5
║  📤 TXs per round  : 50000
║  ─────────────────────────────────────────────────
║  Round 1  TPS      : ~6508 tx/s
║  Round 2  TPS      : ~6856 tx/s
║  Round 3  TPS      : ~6715 tx/s
║  Round 4  TPS      : ~7006 tx/s
║  Round 5  TPS      : ~7077 tx/s
║  ─────────────────────────────────────────────────
║  📉 Min TPS        : ~6508 tx/s
║  📈 Max TPS        : ~7077 tx/s
║  📊 Avg TPS        : ~6832 tx/s
╚═══════════════════════════════════════════════════╝
