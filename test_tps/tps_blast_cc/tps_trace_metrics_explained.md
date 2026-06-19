# 📖 Hướng Dẫn Đọc Bảng `BLOCK PERFORMANCE TRACES`

Bảng `BLOCK PERFORMANCE TRACES` trong tool `tps_blast_cc` giúp bạn mổ xẻ chính xác thời gian được tiêu tốn ở đâu trong toàn bộ vòng đời sinh ra một Block. Đây là công cụ đắc lực nhất để tìm ra "nút thắt cổ chai" (bottleneck) của hệ thống.

Dưới đây là giải thích chi tiết cho từng cột:

### 1. 🧊 `Block` & `TXs`
- **Block:** Số thứ tự của Block đang được thống kê.
- **TXs:** Số lượng giao dịch (Transactions) được đóng gói và xử lý bên trong Block đó. Nhìn vào đây bạn có thể đối chiếu xem số lượng TX ảnh hưởng đến thời gian xử lý như thế nào.

### 2. ⏳ `WaitGo` (Chờ đợi tại Go trước đồng thuận)
- **Đo lường gì?** Thời gian giao dịch nằm chờ ở tầng Go tính từ lúc vừa nhận được (qua RPC/P2P) cho đến khi được gom thành lô (batch) và đẩy sang lõi Rust để chạy đồng thuận.
- **Đánh giá:** Nếu chỉ số này cao, hệ thống tiếp nhận RPC hoặc Mempool của Go đang bị thắt cổ chai (có thể do parse dữ liệu chậm, check chữ ký lâu), khiến giao dịch chưa kịp gửi sang mạng lưới P2P đã bị kẹt lại nội bộ.

### 3. 🦀 `WaitRust` (Thời gian nằm trong lõi Rust / Vòng đời Đồng thuận)
- **Đo lường gì?** Thời gian thực tế tính từ lúc giao dịch "rời khỏi" Go, đi vào lõi Rust, trải qua toàn bộ quá trình đồng thuận BFT/DAG phức tạp với các Node khác trong mạng lưới, cho đến khi được Rust "nhả" lại cho Go (kèm lệnh đóng block). Đây chính là **Round-trip Time** của khâu đồng thuận.
- **Đánh giá:** Đo lường tổng độ trễ của mạng P2P (Network Latency) và tốc độ CPU chạy thuật toán. Mức vài chục đến vài trăm ms cho hàng chục ngàn TPS là lý tưởng. Nếu mạng P2P có vấn đề, thông số này sẽ vọt lên hàng nghìn ms.

### 4. 🤝 `Consensus` (Đồng thuận - Xử lý thuật toán)
- **Đo lường gì?** Thời gian ròng mà Rust dùng để kiểm tra block, chốt sổ danh sách TX và sinh ra Block sau khi gom đủ vote.
- **Đánh giá:** Chỉ số này đo lường áp lực thuật toán lên CPU ở tầng Rust.

### 5. 🌉 `RustFFI` (Độ trễ cầu nối ngôn ngữ)
- **Đo lường gì?** Thời gian "băng qua cầu" FFI (Foreign Function Interface) khi Rust đẩy dữ liệu nguyên khối của hàng vạn giao dịch ngược lại vùng nhớ của Go để thực thi.
- **Đánh giá:** Nếu payload lớn (block to), cầu FFI sẽ tốn thời gian copy data. 

### 6. 📥 `ClientBatch` (Hàng đợi chờ xử lý)
- **Đo lường gì?** Thời gian lô TX này phải nằm chờ trong ống nước (channel) của tầng Execution (Go) trước khi CPU rảnh rỗi để bốc ra xử lý thực thi.
- **Đánh giá:** Nếu cột này cao, luồng xử lý EVM đang làm việc không kịp so với tốc độ nhả Block của Rust, dẫn đến dồn ứ (backpressure) ở tầng Go.

### 7. ⚙️ `ProcessTX` (Thực thi EVM)
- **Đo lường gì?** Thời gian máy ảo EVM chạy code, xác minh chữ ký (lần cuối), tính toán logic Hợp đồng Thông minh, cập nhật số dư cho TẤT CẢ các TX trong block.
- **Đánh giá:** Đây thường là **nút thắt nặng nhất** của Blockchain vì chạy tuần tự. Nếu cột này quá cao (ví dụ >1000ms), bạn cần giới hạn lại `Max TX per block` hoặc nâng cấp cấu hình server.

### 8. 🌳 `CalcRoots` (Tính toán Merkle Roots)
- **Đo lường gì?** Thời gian hệ thống băm (hash) toàn bộ State, Receipts, và TXs để tạo ra các gốc (Roots) bảo mật.
- **Đánh giá:** Nhờ FlatStateTrie và NOMT, hiện tại bước này xử lý cực nhanh (chỉ vài chục ms) dù state có lớn.

### 9. 📦 `BlockData` & `Mapping` (Tạo cấu trúc Block)
- **Đo lường gì?** Thời gian CPU gom các thông tin Roots, Header, Logs vào một Struct `Block` hoàn chỉnh trong RAM và chuẩn bị Mapping data.
- **Đánh giá:** Gần như luôn luôn xấp xỉ `0.xx ms`, bước này rất nhẹ.

### 10. 🧠 `CommitMem` (Lưu vào Cache bộ nhớ)
- **Đo lường gì?** Thời gian đưa các thay đổi State (số dư, biến hợp đồng mới) vào bộ đệm (Memory Database / Cache) trước khi ghi đè xuống ổ cứng.
- **Đánh giá:** Nhanh chóng và ổn định, dao động 10-30ms.

### 11. 💾 `SaveDB` (Ghi xuống ổ cứng - Disk I/O)
- **Đo lường gì?** Thời gian vật lý mà Node phải chép dữ liệu của Block, State, và Receipts xuống cơ sở dữ liệu trên ổ cứng (PebbleDB/LevelDB).
- **Đánh giá:** Phụ thuộc 100% vào tốc độ ổ cứng SSD/NVMe. Nếu chỉ số này thình lình tăng vọt, ổ cứng có thể đang bị thắt nút cổ chai (Disk IOPS Bottleneck). Thông số 10-50ms là rất mượt.

### 12. 🏁 `Total` (Tổng toàn trình xử lý sau Đồng Thuận)
- **Đo lường gì?** Đây là TỔNG CỘNG thời gian của luồng Pipeline bên Go sau khi nhận được khối từ Rust. 
- **Đánh giá:** Nhìn vào `Total`, bạn biết được máy ảo Go mất bao nhiêu ms để nuốt trọn 1 block sau khi mạng lưới chốt sổ.

### 13. 🗑️ `GCPause` (Tạm dừng dọn rác)
- **Đo lường gì?** Quá trình xử lý block sinh ra rác trong RAM, máy ảo Golang phải tự dọn dẹp (Garbage Collection). `GCPause` là tổng thời gian hệ thống bị "đóng băng" (Stop-The-World) để Golang đi dọn rác.
- **Đánh giá:** 
  - `0.0 ms`: Cực kỳ tốt, dọn rác chạy nền mượt.
  - Vài mili-giây (`3.1 ms`): Chấp nhận được.
  - Vài chục đến hàng trăm ms: Báo động đỏ! RAM đang bị rò rỉ (leak) hoặc quá tải object, khiến Golang phải dừng cả node lại rất lâu.

---
💡 **Tóm tắt cách dùng để Tối Ưu Hóa (Tuning):**
- Muốn nâng giới hạn của Block? Nhìn cột **`ProcessTX`**.
- Mạng chạy bị "lag" rớt đồng bộ? Nhìn cột **`Consensus`** và **`ClientBatch`**.
- SSD có đủ nhanh không? Nhìn cột **`SaveDB`**.
- App có bị rò rỉ bộ nhớ (Memory Leak)? Nhìn cột **`GCPause`**.
