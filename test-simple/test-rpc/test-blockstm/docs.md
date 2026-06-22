# Tài liệu mô tả Test Suite Block-STM

Dưới đây là mô tả chi tiết về chức năng và mục đích của 6 thành phần trong thư mục `test-blockstm` dựa trên mã nguồn của chúng. Cả 6 phần này kết hợp lại tạo thành một bộ test suite hoàn chỉnh để kiểm chứng các cơ chế hoạt động cốt lõi của **Block-STM** (cơ chế thực thi giao dịch song song).

### 1. `1-update-same-contract/main.go` (Kiểm tra Cập nhật Đồng thời cùng một Biến State)
- **Mục đích:** Kiểm tra khả năng xử lý của Block-STM khi nhiều giao dịch cùng cố gắng cập nhật (viết) vào cùng một biến trạng thái trên cùng một Smart Contract.
- **Cách hoạt động:** Deploy một contract `Counter`. Các ví được cấu hình sẽ đồng loạt gửi giao dịch gọi hàm `increment()` để tăng giá trị của một biến đếm (count). 
- **Kết quả kỳ vọng:** Do tất cả các giao dịch đều cố gắng ghi đè biến `count` tại cùng một thời điểm, Block-STM phải có khả năng lập lịch, bắt lỗi tranh chấp (conflict detection) và xếp hàng thực thi (hoặc re-execute) chúng một cách chính xác. Kết quả cuối cùng là biến `count` phải bằng đúng tổng số lượng giao dịch đã gửi mà không bị mất mát dữ liệu do race condition.

### 2. `2-read-write/main.go` (Kiểm tra xung đột Đọc - Ghi)
- **Mục đích:** Kiểm tra khả năng xử lý xung đột Read-Write (Đọc-Ghi) của Block-STM.
- **Cách hoạt động:** Deploy contract `ReadWriteConflict`. Sau đó gửi đồng thời nhiều giao dịch:
  - Ví 0 gửi giao dịch **WRITE** gọi `writeData(9999)` để cập nhật biến `sharedData`.
  - Các ví khác gửi giao dịch **READ** gọi `readDataAndSave()` để đọc và lưu lại giá trị `sharedData`.
- **Kết quả kỳ vọng:** Nếu Block-STM phát hiện giao dịch READ chạy trước giao dịch WRITE trong cùng một block, nó phải tự động "hủy bỏ" (abort) và **chạy lại (re-execute)** giao dịch READ đó để đảm bảo nó đọc được giá trị mới nhất (`9999`) thay vì giá trị cũ (`0`). Điều này chứng minh Block-STM đảm bảo tính nhất quán dữ liệu.

### 3. `3-amm-dex/main.go` (Kiểm tra đụng độ cao - High Contention)
- **Mục đích:** Mô phỏng môi trường giao dịch mật độ cực cao (AMM Swap) nơi mọi giao dịch đều tranh chấp cùng một trạng thái.
- **Cách hoạt động:** Deploy contract `AMMSimulator` với thanh khoản ban đầu của Token A và B là 1,000,000. Hàng loạt ví sẽ gửi đồng thời giao dịch `swapAToB` (đổi token A lấy token B).
- **Kết quả kỳ vọng:** Do tất cả các giao dịch đều đọc và ghi vào biến `reserveA` và `reserveB`, sẽ xảy ra xung đột hàng loạt. Block-STM phải có khả năng sắp xếp và xử lý tuần tự (sequential ordering) các giao dịch này ở lớp dưới để bảo toàn đúng công thức AMM Constant Product (tổng dự trữ tính toán phải khớp hoàn toàn sau tất cả các lệnh swap).

### 4. `4-abort/main.go` (Kiểm tra cơ chế Hủy bỏ / Khôi phục)
- **Mục đích:** Kiểm tra cơ chế Abort và Rollback (Hủy và Quay lui trạng thái) của Block-STM khi điều kiện thực thi thay đổi giữa chừng.
- **Cách hoạt động:** Deploy contract `AbortRollback`. 
  - Ví 0 gửi giao dịch đổi trạng thái `setPhase(2)`.
  - Các ví khác gửi đồng thời giao dịch `updateIfPhase1(888)`, hàm này yêu cầu `require(phase == 1)`.
- **Kết quả kỳ vọng:** Block-STM phát hiện giao dịch `setPhase(2)` làm thay đổi điều kiện đầu vào của các giao dịch `updateIfPhase1` đang chạy song song. Nó sẽ tự động **rollback** các giao dịch `updateIfPhase1` và đánh dấu chúng là REVERT. Điều này xác minh Block-STM không bỏ lọt các lỗi logic khi trạng thái bị thay đổi bởi giao dịch khác.

### 5. `5-gas/main.go` (Kiểm tra cách tính phí Gas khi Re-execute)
- **Mục đích:** Kiểm tra lượng Gas bị trừ khi Block-STM buộc phải chạy lại (Re-execute) hoặc từ chối (Revert) một giao dịch.
- **Cách hoạt động:** Tái sử dụng contract `AbortRollback` và kịch bản gửi giao dịch giống hệt test `4-abort`. Tuy nhiên, thay vì chỉ xem xét trạng thái thành công/thất bại, test này trích xuất `GasUsed` từ Transaction Receipt.
- **Kết quả kỳ vọng:** Chứng minh rằng dù Block-STM có âm thầm chạy đi chạy lại một giao dịch (re-execute) nhiều lần dưới background để giải quyết xung đột, **người dùng chỉ bị trừ Gas đúng 1 lần duy nhất** cho kết quả thực thi cuối cùng. Giao dịch revert vẫn sẽ tốn một lượng gas cơ bản (base gas + phí thực thi đến điểm bị revert), và không bao giờ vượt quá logic máy ảo EVM thông thường.

### 6. `contracts/BlockSTMTests.sol` (Smart Contracts cho Test Suite)
- **Mục đích:** Chứa toàn bộ logic On-chain (Mã nguồn Solidity) cho các bài test từ 2 đến 5.
- **Thành phần:**
  - `Contract ReadWriteConflict`: Phục vụ bài test 2, chứa hàm `writeData` và `readDataAndSave`.
  - `Contract AMMSimulator`: Phục vụ bài test 3, mô phỏng cơ chế Swap của DEX với hàm `swapAToB`.
  - `Contract AbortRollback`: Phục vụ bài test 4 & 5, chứa logic đổi trạng thái bằng `setPhase` và logic điều kiện bằng `updateIfPhase1`.
