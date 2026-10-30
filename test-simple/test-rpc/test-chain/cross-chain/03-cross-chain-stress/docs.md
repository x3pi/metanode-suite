# Kịch Bản Test 03: Stress Test Giao Dịch Xuyên Chuỗi Đồng Thời (Parallel Cross-Chain Stress)

## 1. Mục Đích
Bài test này đánh giá năng lực chịu tải và độ ổn định của hệ thống cross-chain khi có nhiều giao dịch xuyên chuỗi được phát sóng đồng thời (concurrent / parallel).

Mục tiêu chính:
- Kiểm tra Block-STM và hệ thống hàng đợi giao dịch cross-chain có bị race condition hay deadlock không.
- Đo lường độ trễ xử lý khi chịu tải song song.
- Xác thực trạng thái của cụm Parent Chain sau khi tiếp nhận và xác thực nhiều thông điệp cross-chain cùng lúc.

## 2. Quy Trình Kiểm Tra

1. **Khởi tạo tải song song**:
   - Sử dụng Goroutines để phát sóng đồng thời một tập hợp các giao dịch xuyên chuỗi (10 giao dịch) từ Execution Cluster 1 sang Execution Cluster 2.
   - Mỗi giao dịch chuyển đến một địa chỉ ví ngẫu nhiên độc lập để kiểm tra khả năng xử lý song song không xung đột trạng thái ví.

2. **Ghi nhận hiệu năng**:
   - Theo dõi từng TxHash, mã lỗi (nếu có), và thời gian thực thi (latency) của từng giao dịch.
   - Thống kê tổng thời gian hoàn tất đợt gửi tải.

3. **Hậu kiểm Parent Chain (Post-load Verification)**:
   - Truy vấn lại toàn bộ các node Parent Chain để xác nhận không có node nào bị crash, fork hay lag block sau đợt tải.

## 3. Cách Thực Hiện

### Chạy qua script tổng
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain
./run_tests.sh 03
```

### Chạy qua go run
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/03-cross-chain-stress
go run .
```

### Chạy qua go test
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/03-cross-chain-stress
go test -v .
```
