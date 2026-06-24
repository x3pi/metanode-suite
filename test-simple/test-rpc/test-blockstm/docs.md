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

### 6. `6-native-many-to-one/main.go` (Kiểm tra Chuyển tiền Native Đồng thời - Many to One)
- **Mục đích:** Kiểm tra xử lý đồng thời khi nhiều ví cùng chuyển tiền Native (đồng coin cơ sở) vào một ví nhận duy nhất.
- **Cách hoạt động:** Lấy ví 0 làm ví nhận. Tất cả các ví còn lại (từ 1 đến 9) đồng loạt tạo giao dịch chuyển một lượng wei (Native Transfer) tới ví nhận này cùng một lúc.
- **Kết quả kỳ vọng:** Do giao dịch Native được xử lý trong Fast-Path (không chạy qua Block-STM nếu không có EVM Tx) hoặc Block-STM (nếu có EVM Tx đi kèm), hệ thống phải khoá và cộng dồn số dư của ví nhận một cách chính xác. Kết quả số dư cuối cùng của ví nhận phải tăng đúng bằng tổng số tiền mà các ví đã gửi mà không bị mất mát do Race Condition.

### 7. `7-native-one-to-many/main.go` (Kiểm tra Mempool với Nonce tự tăng - One to Many)
- **Mục đích:** Kiểm tra khả năng xử lý của Mempool và mạng lưới khi một ví gửi ồ ạt nhiều giao dịch liên tiếp.
- **Cách hoạt động:** Lấy ví 0 làm ví gửi. Ví 0 tự tính toán nonce tăng dần và liên tục tạo nhiều giao dịch (mỗi giao dịch gửi tới một ví nhận khác nhau từ 1 đến 9). Tất cả các giao dịch này được đẩy (push) vào Mempool gần như cùng một tích tắc.
- **Kết quả kỳ vọng:** Mempool phải có khả năng tiếp nhận, sắp xếp đúng thứ tự các giao dịch theo Nonce từ thấp đến cao của cùng một sender. Sau đó, chuỗi khối phải gắp và thực thi thành công toàn bộ các giao dịch này vào block mà không từ chối (reject) nhầm do Nonce out-of-order.

### 8. `8-native-mixed-evm/main.go` (Kiểm tra xen kẽ Native Transfer và Smart Contract Call)
- **Mục đích:** Kiểm tra đường biên (boundary) và cơ chế chuyển đổi luồng xử lý của hệ thống Execution giữa Fast-Path và Block-STM.
- **Cách hoạt động:** Gửi đồng thời hai loại giao dịch: Một giao dịch gọi hàm của Smart Contract (EVM Call) và hàng loạt giao dịch chuyển tiền Native thông thường.
- **Kết quả kỳ vọng:** Theo thiết kế của Metanode, khi có bất kỳ một giao dịch EVM nào xuất hiện trong block, toàn bộ các giao dịch (bao gồm cả Native Transfer) sẽ bị buộc phải đưa vào cỗ máy Block-STM để phân tích xung đột và thực thi song song. Test này đảm bảo Block-STM xử lý mượt mà và chuẩn xác các giao dịch Native thông thường bên cạnh các giao dịch hợp đồng thông minh phức tạp.

### 9. `9-cross-contract-call/main.go` (Kiểm tra Gọi hợp đồng lồng nhau - Internal Calls)
- **Mục đích:** Kiểm tra tính chính xác của Block-STM khi xử lý các cuộc gọi lồng nhau giữa các Smart Contract.
- **Cách hoạt động:** Deploy `TargetContract` và `CallerContract`. Hàng loạt ví sẽ gửi đồng thời giao dịch gọi `CallerContract.callTarget(value)`. Bên trong `CallerContract`, nó sẽ tiếp tục thực hiện một cuộc gọi EVM (internal call) sang `TargetContract.addValue(value)`.
- **Kết quả kỳ vọng:** Block-STM phải có khả năng bóc tách, tracking (theo dõi) được các truy cập Storage sâu bên trong `TargetContract` dù giao dịch gốc chỉ gọi `CallerContract`. Do tất cả các giao dịch đều cố gắng thay đổi cùng một biến `value` của `TargetContract`, hệ thống phải bắt được lỗi xung đột, gom nhóm và xử lý tuần tự một cách chuẩn xác, đảm bảo kết quả cuối cùng phải bằng đúng tổng của tất cả các `value` được gửi tới (cộng dồn).

### 10. `10-cross-contract-payable/main.go` (Kiểm tra Chuyển tiền lồng nhau - Internal Payable Calls)
- **Mục đích:** Là bài test toàn diện nhất, kiểm tra đồng thời khả năng xử lý xung đột Storage và thay đổi Số dư (Balance/ETH) trong các luồng gọi nội bộ của EVM.
- **Cách hoạt động:** Tương tự Test 9 nhưng sử dụng `PayableTargetContract` và `PayableCallerContract`. Mỗi giao dịch không chỉ mang theo data mà còn đính kèm tiền (Native ETH/wei). `CallerContract` sẽ nhận tiền và forward toàn bộ số tiền đó sang `TargetContract` thông qua internal call.
- **Kết quả kỳ vọng:** Block-STM phải xử lý hoàn hảo cả 3 lớp:
  1. **Storage 1 (`value += 1`)**: Tổng số lượt gọi phải khớp với số lượng giao dịch.
  2. **Storage 2 (`totalEthReceived += msg.value`)**: Biến lưu trữ tổng số tiền nội bộ phải khớp với tổng số ETH đã gửi.
  3. **Global Balance**: Số dư Native thực tế của `TargetContract` trên sổ cái blockchain phải khớp hoàn toàn với số ETH đã nhận, không được thất thoát cũng không được nhân đôi (Double Minting).

### 11. `contracts/BlockSTMTests.sol` (Smart Contracts cho Test Suite)
- **Mục đích:** Chứa toàn bộ logic On-chain (Mã nguồn Solidity) cho các bài test từ 2 đến 10.
- **Thành phần:**
  - `Contract ReadWriteConflict`: Phục vụ bài test 2 và test 8.
  - `Contract AMMSimulator`: Phục vụ bài test 3.
  - `Contract AbortRollback`: Phục vụ bài test 4 & 5.
  - `Contract DepositContract`: Phục vụ lưu trữ tiền cơ bản.
  - `Contract TargetContract` & `CallerContract`: Phục vụ bài test 9 (Cross-Contract Calls).
  - `Contract PayableTargetContract` & `PayableCallerContract`: Phục vụ bài test 10 (Cross-Contract Payable).
