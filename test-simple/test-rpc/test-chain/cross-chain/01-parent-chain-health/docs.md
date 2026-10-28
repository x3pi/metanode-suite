# Kịch Bản Test 01: Kiểm Tra Sức Khỏe Toàn Diện Parent Chain

## 1. Mục Đích
Bài test này kiểm tra tính sẵn sàng, độ ổn định và tính toàn vẹn của mạng lưới đồng thuận gốc (Parent Chain / Root Consensus Chain) trước và trong khi các Execution Shards thực hiện giao dịch xuyên chuỗi.

Parent Chain đóng vai trò mỏ neo (root anchor) để đăng ký các Execution Clusters, ghi nhận trạng thái khối chéo và duy trì đồng thuận Byzantine Fault Tolerant (BFT).

## 2. Các Hạng Mục Kiểm Tra

1. **Kết nối mạng & Độ trễ (Connectivity & Latency)**:
   - Truy vấn endpoint `/status` trên từng node Parent Chain.
   - Đo thời gian phản hồi (RTT/latency tính bằng millisecond).
   - Kiểm tra mã phản hồi HTTP 200 và dữ liệu JSON hợp lệ.

2. **Trạng thái đồng bộ & Ngăn chặn rẽ nhánh (Syncing & Fork Detection)**:
   - `syncing: false`: Đảm bảo node đã hoàn tất đồng bộ với các peer khác.
   - `fork_detected: false`: Đảm bảo không có xung đột rẽ nhánh giữa các validator.

3. **Cơ chế BFT Quorum (2f + 1)**:
   - Với tổng số validator N:
     - Số lỗi Byzantine tối đa cho phép: f = (N - 1) / 3
     - Quorum tối thiểu để chuỗi sinh block hợp lệ: 2f + 1
   - Kiểm tra số lượng node thực tế đang online có đạt hoặc vượt mức quorum này không.

4. **Độ lệch chiều cao khối giữa các node (Height Synchronization Divergence)**:
   - Thu thập chiều cao khối hiện tại (Height) của tất cả các node online.
   - Tính toán độ lệch giữa node cao nhất và node thấp nhất: delta = max(Height) - min(Height).
   - Ngưỡng cho phép: delta <= 2 (đạt chuẩn đồng bộ thời gian thực).

5. **Tiến trình sinh khối (Block Progression Liveness)**:
   - Ghi nhận chiều cao ban đầu của node.
   - Chờ một khoảng thời gian (4 giây) và đo lại chiều cao.
   - Xác nhận node tiếp tục tiến về phía trước (liveness) và không bị deadlock đồng thuận.

## 3. Cách Thực Hiện

### Chạy trực tiếp qua script tổng
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain
./run_tests.sh 01
```

### Chạy qua go run
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/01-parent-chain-health
go run .
```

### Chạy qua go test
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/01-parent-chain-health
go test -v .
```

## 4. Cấu Hình Nạp Mạng
Test tự động phát hiện danh sách node thông qua:
1. File topology: `/tmp/rpc_nodes.json` (sinh bởi Ansible deployment).
2. Biến môi trường: `RPC_NODES_FILE`, `PARENT_CHAIN_URL`.
3. Cấu hình mặc định: `http://127.0.0.1:18601`, `18602`, `18603`, `18604`.
