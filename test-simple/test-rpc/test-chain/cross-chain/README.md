# Bộ Test Cross-Chain & Parent Chain Health - MetaNode Suite

Thư mục này chứa bộ công cụ và các kịch bản kiểm thử (test suite) cho giao dịch xuyên chuỗi (**Cross-Chain Transactions**) và kiểm tra toàn diện sức khỏe chuỗi đồng thuận gốc (**Parent Chain Consensus Health**).

---

## 1. Cấu Trúc Thư Mục

Mỗi bài test được đặt độc lập trong từng thư mục riêng biệt kèm tài liệu hướng dẫn (`docs.md`), mã nguồn thực thi (`main.go`) và unit test (`*_test.go`):

```text
cross-chain/
├── common/                     # Thư viện dùng chung giữa các test
│   ├── config.go               # Tự động nạp cấu hình mạng từ /tmp/rpc_nodes.json / ENV
│   ├── rpc_client.go           # Client tương tác HTTP /status, JSON-RPC, balances, etc.
│   └── ui.go                   # Định dạng màu sắc và bảng in kết quả
│
├── 01-parent-chain-health/     # Bài test 01: Kiểm tra sức khỏe toàn diện cụm Parent Chain
│   ├── main.go                 # Thực thi độc lập (go run .)
│   ├── parent_health_test.go   # Go testing runner (go test -v .)
│   └── docs.md                 # Tài liệu chi tiết kịch bản test 01
│
├── 02-cross-chain-transfer/    # Bài test 02: Chuyển tiền xuyên chuỗi hai chiều
│   ├── main.go                 # Gửi giao dịch Exec 1 <-> Exec 2, kiểm tra bảo toàn số dư
│   ├── cross_chain_test.go     # Go testing runner
│   └── docs.md                 # Tài liệu chi tiết kịch bản test 02
│
├── 03-cross-chain-stress/      # Bài test 03: Stress test phát sóng đồng thời nhiều tx
│   ├── main.go                 # Phát sóng song song 10 giao dịch xuyên chuỗi
│   ├── stress_test.go          # Go testing runner
│   └── docs.md                 # Tài liệu chi tiết kịch bản test 03
│
├── run_tests.sh                # Script bash điều khiển chạy linh hoạt toàn bộ hoặc từng bài
└── README.md                   # Tài liệu tổng quan (file này)
```

---

## 2. Cơ Chế Tự Động Nhận Diện Mạng (Auto-Discovery)

Hệ thống tự động phát hiện danh sách các node đang chạy thông qua `common/config.go`:
1. **File topology cluster**: Đọc trực tiếp file `/tmp/rpc_nodes.json` do Ansible sinh ra khi deploy. Nhận diện chính xác IP của các node dù nằm trên nhiều máy chủ khác nhau (multi-server).
2. **Biến môi trường**: Cho phép ghi đè linh hoạt qua `PARENT_CHAIN_URL`, `EXEC1_URL`, `EXEC2_URL`, `RPC_NODES_FILE`.
3. **Giá trị dự phòng mặc định (Fallback)**:
   - Parent Chain Nodes: `http://127.0.0.1:18601`, `18602`, `18603`, `18604`
   - Execution Cluster 1: `http://127.0.0.1:8646` (Chain ID 9001)
   - Execution Cluster 2: `http://127.0.0.1:8647` (Chain ID 9002)

---

## 3. Hướng Dẫn Sử Dụng Script `run_tests.sh`

`run_tests.sh` là công cụ trung tâm để chạy các bài test. Script có thể tự động nhận diện tất cả các thư mục có tiền tố số (`01-*`, `02-*`, `03-*`, ...):

### 3.1. Chạy Tất Cả Các Bài Test
```bash
./run_tests.sh
# hoặc:
./run_tests.sh all
```

### 3.2. Chạy Từng Bài Test Cụ Thể
Bạn có thể chỉ định theo số thứ tự hoặc từ khóa gợi nhớ:
```bash
# Chạy Test 01 (Parent Chain Health)
./run_tests.sh 01
./run_tests.sh parent

# Chạy Test 02 (Cross-Chain Transfer)
./run_tests.sh 02
./run_tests.sh transfer

# Chạy Test 03 (Parallel Stress Test)
./run_tests.sh 03
./run_tests.sh stress
```

### 3.3. Chạy Với Chế Độ Unit Test (`go test`)
Thêm cờ `--gotest` hoặc `-test` để chạy bằng Go test runner:
```bash
# Chạy unit test của bài 01
./run_tests.sh 01 --gotest

# Chạy unit test toàn bộ các bài
./run_tests.sh all --gotest
```

---

## 4. Chi Tiết Các Kịch Bản Test

### Test 01: Sức Khỏe Toàn Diện Parent Chain (`01-parent-chain-health`)
- **Kết nối & Latency**: Truy vấn `/status` trên từng node, đo độ trễ mạng ms.
- **Trạng thái đồng bộ**: Đảm bảo `syncing: false` và `fork_detected: false`.
- **BFT Quorum (2f + 1)**: Với N validator, kiểm tra số node online đạt tối thiểu 2f + 1 (với f = (N - 1) / 3).
- **Độ lệch khối (Height Divergence)**: Đảm bảo chênh lệch block giữa các node không vượt quá 2 block.
- **Tiến trình sinh khối (Liveness)**: Đo lường tiến độ tăng chiều cao khối trong thời gian thực.

### Test 02: Chuyển Tiền Xuyên Chuỗi Hai Chiều (`02-cross-chain-transfer`)
- **Kiểm tra trạng thái Execution Clusters**: Kết nối đến Exec 1 (Chain 9001) và Exec 2 (Chain 9002).
- **Giao dịch Exec 1 -> Exec 2**: Phát sóng giao dịch `mtn_sendCrossChainTransfer`, kiểm tra sinh `txHash` và số dư đích.
- **Giao dịch Exec 2 -> Exec 1**: Xác thực luân chuyển tài sản theo chiều ngược lại.
- **Bảo toàn số dư (Conservation)**: Truy vấn `mtn_getConservation` để kiểm tra tổng cung không bị thất thoát hoặc nhân đôi.

### Test 03: Stress Test Phát Sóng Đồng Thời (`03-cross-chain-stress`)
- **Song song hóa**: Gửi đồng thời 10 giao dịch xuyên chuỗi qua Goroutines.
- **Đo lường hiệu năng**: Ghi nhận thời gian phản hồi từng giao dịch và tổng thời gian hoàn tất.
- **Hậu kiểm**: Kiểm tra cụm Parent Chain vẫn duy trì quorum và sinh khối ổn định sau tải.

---

## 5. Hướng Dẫn Thêm Bài Test Mới Trong Tương Lai

Khi cần bổ sung thêm bài test mới (ví dụ: `04-cross-chain-smart-contract`):
1. Tạo thư mục mới theo quy ước: `04-<ten-bai-test>/`
2. Tạo file `main.go` và `*_test.go`.
3. Import thư viện dùng chung:
   ```go
   import "tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
   ```
4. Viết file `docs.md` mô tả mục đích và cách chạy của bài test mới.
5. Script `run_tests.sh` sẽ **tự động phát hiện** thư mục `04-*` mà không cần sửa code của script! Bạn có thể chạy ngay bằng:
   ```bash
   ./run_tests.sh 04
   ```
