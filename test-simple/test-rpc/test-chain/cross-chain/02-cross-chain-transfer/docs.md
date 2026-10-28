# Kịch Bản Test 02: Giao Dịch Chuyển Tiền Xuyên Chuỗi (Cross-Chain Transfer)

## 1. Mục Đích
Bài test này xác thực khả năng luân chuyển tài sản hai chiều (bi-directional cross-chain transfer) giữa các phân mảnh thực thi (Execution Shards/Clusters) thông qua lớp đồng thuận mỏ neo Parent Chain.

Các chuỗi thực thi kiểm tra mặc định:
- **Execution Chain 1** (Chain ID 9001, RPC mặc định `http://127.0.0.1:8646`)
- **Execution Chain 2** (Chain ID 9002, RPC mặc định `http://127.0.0.1:8647`)

## 2. Quy Trình Kiểm Tra

1. **Kiểm tra khả năng kết nối (Cluster Reachability)**:
   - Truy vấn `eth_blockNumber` trên cả hai Execution Clusters.
   - Xác nhận node lắng nghe và phản hồi RPC JSON-RPC 2.0.

2. **Chuyển tiền chiều thuận (Exec 1 -> Exec 2)**:
   - Tạo ngẫu nhiên một địa chỉ ví nhận hợp lệ (20-byte Ethereum format).
   - Truy vấn số dư ban đầu trên Exec 2 qua `mtn_getAccountState`.
   - Gửi giao dịch xuyên chuỗi từ Exec 1 qua phương thức `mtn_sendCrossChainTransfer(targetAddress, amount)`.
   - Kiểm tra mã hash giao dịch (txHash) trả về.
   - Chờ chuyển tiếp (relay) và kiểm tra số dư đích đã cập nhật.

3. **Chuyển tiền chiều nghịch (Exec 2 -> Exec 1)**:
   - Thực hiện luồng tương tự từ Exec 2 chuyển về ví nhận trên Exec 1.
   - Xác nhận cơ chế hai chiều hoạt động đối xứng.

4. **Kiểm tra bảo toàn tổng cung (Conservation Check)**:
   - Truy vấn endpoint `mtn_getConservation` trên cả hai cluster.
   - Đảm bảo việc mint/burn token giữa hai chuỗi khớp nhau, không có token sinh ra từ hư không.

## 3. Cách Thực Hiện

### Chạy qua script tổng
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain
./run_tests.sh 02
```

### Chạy qua go run
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/02-cross-chain-transfer
go run .
```

### Chạy qua go test
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/02-cross-chain-transfer
go test -v .
```
