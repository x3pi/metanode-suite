# Nâng cấp True Block-STM: Dependency-Aware Suspend & Wakeup

Kế hoạch này nhằm mục đích loại bỏ hiện tượng Goroutine Thrashing (Busy-Wait/Spin-abort) khi xử lý xung đột transaction trong True Block-STM của Metanode. Cơ chế Suspend & Wakeup sẽ giúp hệ thống đạt hiệu suất (TPS) tối đa bằng cách cho các luồng "ngủ" thực sự (giải phóng Goroutine) khi chờ tài nguyên và chỉ đánh thức khi tài nguyên đã sẵn sàng.

## Architecture Decisions

> [!TIP]
> **Quyết định Concurrency (Tối đa hóa TPS):** 
> Hệ thống sẽ sử dụng mảng tĩnh kết hợp `sync.Mutex` trên từng index (`[]sync.Mutex`). 
> **Lý do:** Khóa hạt mịn (Fine-grained locking) trên mảng tĩnh có độ trễ cực thấp (O(1) memory access) và không sinh ra rác cho Garbage Collector (zero-allocation) so với việc dùng Channel hay Linked-list lock-free. Điều này đảm bảo TPS cao nhất trong môi trường Block-STM của Golang.

> [!IMPORTANT]
> **Thay đổi kiến trúc Wrapper:** Để cơ chế này hoạt động, đối tượng `MVCCAccountStateDB` và `MVCCSmartContractDB` cần bắt được chính xác `Version` (txIndex) của giao dịch đang giữ `EstimateMarker` và trả về cho luồng thực thi chính.

## Proposed Changes

---

### [metanode/execution/pkg/blockchain/tx_processor/mvcc/]

#### [MODIFY] [mvcc_account.go](/metanode/execution/pkg/blockchain/tx_processor/mvcc/mvcc_account.go)
- **Thay đổi:** Cập nhật các hàm đọc (`Get`, `Read`) để khi gặp `ErrEstimateHit`, chúng lưu lại `BlockingVersion` (phiên bản chặn) vào struct `MVCCAccountStateDB`.
- **Mục đích:** Để báo cho Block-STM biết chính xác giao dịch nào (txIndex nào) đang chặn đường.

#### [MODIFY] [mvcc_contract.go](/metanode/execution/pkg/blockchain/tx_processor/mvcc/mvcc_contract.go)
- **Thay đổi:** Tương tự như account, cập nhật `MVCCSmartContractDB` để bắt và lưu lại `BlockingVersion` khi gặp `ErrEstimateHit` trên Storage Map.

---

### [metanode/execution/pkg/blockchain/tx_processor/]

#### [MODIFY] [true_block_stm.go](/metanode/execution/pkg/blockchain/tx_processor/true_block_stm.go)
- **Thêm Cấu trúc WaitList:**
  - Bổ sung `waiters [][]uint32` và `waitersMu []sync.Mutex` vào `TrueBlockSTM` struct. Độ dài mảng bằng số lượng giao dịch (`numTxs`).
- **Cập nhật quá trình Khởi tạo (NewTrueBlockSTM):**
  - Khởi tạo mảng `waiters` và `waitersMu`.
- **Cập nhật Logic Treo (Suspend) trong Execution Dispatcher:**
  - Nếu quá trình thực thi trả về `ErrEstimateHit` (lỗi đụng EstimateMarker).
  - Lấy `BlockingVersion` từ DB Wrapper.
  - Khóa `waitersMu[BlockingVersion]`, thêm `txIndex` hiện tại vào `waiters[BlockingVersion]`, rồi mở khóa.
  - **Lưu ý quan trọng:** Không đẩy `txIndex` vào `execIn` nữa. Dừng Goroutine hiện tại bằng lệnh `return`.
- **Cập nhật Logic Đánh thức (Wakeup) sau Execution & Validation:**
  - Khi một giao dịch (`txIndex`) thực thi thành công (đổi state sang 1), HOẶC khi nó bị Abort (đổi state sang 4) và dọn dẹp xong `EstimateMarker` của nó.
  - Nó phải khóa `waitersMu[txIndex]`, lấy toàn bộ danh sách các giao dịch đang chờ mình (`waiters[txIndex]`), sau đó xóa sạch danh sách này.
  - Duyệt qua từng giao dịch được lấy ra và đẩy chúng ngược lại vào `execIn` (kích hoạt chạy lại).

## Verification Plan

### Automated Tests
- Chạy các script test Unit Test trong thư mục `execution/debug_nil/` để đảm bảo không bị nil pointer panic.
- Chạy `build_check.sh` trong `consensus/metanode/scripts/` để đảm bảo code Golang và CGO (Rust/C++) build thành công 100%.

### Manual Verification
- Deploy node ở chế độ giả lập (test-tps) hoặc bơm một lượng lớn giao dịch hợp đồng thông minh (Smart Contract) có xung đột (cùng truy cập vào 1 account/storage).
- Quan sát CPU usage và logs: Sẽ không còn thấy thông báo Cảnh báo (Warn) Abort liên tục lặp lại quá nhanh, TPS sẽ ổn định hơn và ít tốn CPU hơn hẳn so với nhánh main/fix-block hiện tại.
