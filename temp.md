Ran command: `./auto_test.sh`
Ran command: `go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748"`
Viewed main.go:249-290
Ran command: `./restore_node.sh  1`

Đây là hiện tượng **Phân nhánh chuỗi (Chain Fork)** kèm theo lỗi **"CHAIN BROKEN" (đứt gãy chuỗi)** trong nội bộ node. Cụ thể chuyện gì đã xảy ra:

**1. Khôi phục snapshot (m1) diễn ra bình thường:**
Khi bạn chạy script restore node 1, node này (m1) đã bị tắt, lỗi connection refused được phát hiện, log cache được dọn, và sau đó m1 đã đồng bộ dữ liệu snapshot lại thành công từ block 1064 cho tới 1071 một cách hoàn hảo (block 1072 cả 5 nodes đều có mã hash khớp 100%).

**2. Phân nhánh (Fork) xảy ra tại block 1073:**
Tại block 1073, hệ thống đã sinh ra một block rỗng (`EMPTY BLOCK`, tx=0, sys_tx=0). Lúc này, cụm node bị chia làm 2 phe:
- **Phe 1 (m1, m2, m3):** Sinh ra block 1073 với hash `0xffec140...`
- **Phe 2 (m0, m4):** Sinh ra block 1073 với hash `0xea5bb9a...`

👉 *Đáng chú ý:* Mặc dù mã hash của block khác nhau, nhưng `stateRoot`, `gei` và `epoch` của 2 phe đều hoàn toàn **GIỐNG NHAU** (`stateRoot = 0x007800cbf...`). Điều này chứng tỏ logic State không bị sai lệch, nhưng mã hash khác nhau có thể do các node gán `timestamp` khác nhau hoặc Validator ký block khác nhau khi tạo empty block.

**3. Tại sao sinh ra lỗi CHAIN BROKEN trên m4?**
Sự phân nhánh này kéo dài tới block 1074. Nhưng đến **block 1075**, node `m4` đột nhiên nhận và ghi nhận block 1075 của phe 1 (do m1/m2/m3 gửi sang).
Lúc này trên `m4`:
- Block **1074** (cũ của m4) có hash là: `0x98530...` (thuộc nhánh phe 2)
- Block **1075** (mới nhận) lại trỏ tới `parentHash`: `0x2a796...` (thuộc nhánh phe 1)

Việc block mới không trỏ về block cũ ngay phía trước mặt nó khiến chuỗi tại `m4` bị đứt gãy vật lý (Chain Broken). Node `m4` đã "nhảy" sang nhánh khác nhưng **chưa chịu rollback (hủy) block 1074 và 1073 bị sai trước đó**.

**💡 Tổng kết nguyên nhân:**
Việc khởi động lại node từ snapshot (hoặc do tải nặng từ `auto_test.sh`) có thể đã làm xáo trộn nhẹ mạng lưới P2P hoặc làm chậm quá trình broadcast, khiến thuật toán đồng thuận (Consensus) tạo ra 2 phiên bản block rỗng khác nhau. Cơ chế "giải quyết fork" (Fork Resolution / Rollback) của hệ thống hiện tại chưa xử lý triệt để việc đảo nhánh, khiến một node (m4) bị dính block lơ lửng.