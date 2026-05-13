# Luồng Khởi Động & Đồng Bộ Bắt Kịp Mạng (Catch-up Sync) Của Metanode

Tài liệu này mô tả chi tiết quá trình một Node khởi động lại (restart) sau khi bị tắt ngang 1-3 Epoch mà **không sử dụng Snapshot**. Quá trình này bao gồm việc tự chữa lành cơ sở dữ liệu (Self-healing) và đồng bộ tốc độ cao (Catch-up) với mạng lưới.

---

## Giai đoạn 1: Khởi động Go và Tự phục hồi (Self-Healing)
**Vị trí Code:** `execution/cmd/simple_chain/app_blockchain.go` (Hàm `initBlockchain`)

Khi bạn khởi động lại Node, tiến trình Go (`go-master`) sẽ khởi chạy đầu tiên. Vì bạn KHÔNG copy `metadata.json` mới vào thư mục, biến `metadata` sẽ bằng `nil`. Hệ thống sẽ đi vào cơ chế kiểm tra tính toàn vẹn của dữ liệu:

1. **So sánh Root:** Node đọc `startStateRoot` từ LevelDB (chứa block P2P tải về) và `nomtRoot` từ Database NOMT (chứa trạng thái EVM đã thực thi).
2. **Kích hoạt Fallback (Two-Phase Scan):** Nếu Node trước đó bị tắt nóng/sập nguồn lúc đang ghi dữ liệu, LevelDB sẽ vượt mặt NOMT (`startStateRoot != nomtRoot`). Hệ thống lập tức nhảy vào khối `else if` tại dòng 511.
3. **Quét lùi (Backward Scan):** 
   - Node quét ngược chuỗi block từ hiện tại về quá khứ để tìm block cuối cùng có `StateRoot` khớp đúng với `nomtRoot` đang có trong ổ cứng.
   - Khi tìm thấy (ví dụ: lùi từ block 245 xuống 239), nó ép hệ thống (Go) dùng Block 239 làm điểm xuất phát mới `startLastBlock = current`.
4. **Kết quả Giai đoạn 1:** Cấu trúc dữ liệu cục bộ được chữa lành. Go mở kết nối Unix Socket (FFI) và sẵn sàng chờ Rust.

---

## Giai đoạn 2: Khởi động Rust & Phát hiện tụt hậu (SyncOnly)
**Vị trí Code:** Lớp Consensus (Rust) `consensus/metanode/src/node/`

Ngay sau khi Go ổn định ở mốc GEI (Global Exec Index) an toàn (Ví dụ: 239), thành phần Rust bắt đầu chạy:

1. **Đọc trạng thái từ Go:** Rust gọi qua FFI hỏi Go *"Mày đang ở block bao nhiêu?"* -> Go trả lời là GEI=239, Epoch=2.
2. **Khoá hệ thống (Lock Proposals):** Rust kết nối với các Peer khác trong mạng và nhận ra mốc chung của mạng đã ở GEI=677 (chạy xa tít rồi). Nó lập tức in log `[STARTUP-SYNC] Proposals LOCKED` và chuyển Node sang trạng thái **`SyncOnly`**. Ở trạng thái này, Node chỉ im lặng tải dữ liệu, không tham gia biểu quyết để tránh làm hỏng mạng.
3. **Kéo dữ liệu (Fetch):** Hàm `fetch_commits()` của Rust chạy hết tốc lực tải các khối (Commit) còn thiếu từ Node 2, Node 3 về.

---

## Giai đoạn 3: Bơm dữ liệu qua FFI & Thực thi siêu tốc (Fast-Path)
**Vị trí Code:** Giao tiếp FFI & `execution/cmd/simple_chain/processor/block_processor_sync.go`

Đây là giai đoạn Node "bắt kịp" mạng lưới thực sự:

1. **Bơm khối FFI:** Cứ tải được Commit nào, hàm `send_commits_to_go()` của Rust lại bắn cục byte thẳng qua RAM sang hàm `ExecuteBlockStream()` của Go.
2. **Xử lý tại Go (`processSingleEpochData`):** Go hứng dữ liệu và bắt đầu phân tích:
   - **FAST-PATH (Siêu tốc):** Nếu Commit tải về là rỗng (0 transaction) - điều rất hay xảy ra trong Blockchain - Go sẽ **bỏ qua hoàn toàn bước chạy EVM**. Nó chỉ đơn giản là cộng thêm 1 vào biến đếm GEI và nhảy qua Commit tiếp theo. Nhờ điều này, Node đồng bộ hàng ngàn block rỗng chỉ trong 1 tích tắc.
   - **Xử lý Epoch (Epoch-Inflation Guard):** Nếu Go phát hiện cục dữ liệu ghi nhận Epoch lớn hơn hiện tại (Ví dụ đang ở Epoch 2 nhưng dữ liệu là Epoch 3), nó tự động gọi hàm `CheckAndUpdateEpochFromBlock()` để sang trang lịch sử mới và reset các bộ đếm.
   - **Thực thi giao dịch:** Nếu Commit có giao dịch thật, Go chạy giao dịch đó qua Máy ảo (EVM), cập nhật số dư, và ghi xuống NOMT.

---

## Giai đoạn 4: Hoàn tất đồng bộ & Trở lại làm Validator
**Vị trí Code:** Cả Rust và Go

Quá trình lặp lại vòng lặp ở Giai đoạn 3 với tốc độ chóng mặt (Go GEI advanced: 239 -> 437 -> 677).
1. Khi biến `local_gei` của Go bằng đúng với GEI của toàn mạng lưới (Ví dụ: 677).
2. Rust nhận ra đã đuổi kịp mạng (`Local state in sync`).
3. Rust mở khoá hệ thống (`Proposals UNLOCKED`), tự động rũ bỏ thân phận `SyncOnly` và thăng cấp trở lại làm **`Validator`** (nếu nó nằm trong danh sách uỷ ban Epoch đó).
4. Node bắt đầu biểu quyết cho các block mới như chưa hề có cuộc sập nguồn nào xảy ra.

> **Tổng kết:** Thiết kế FFI kết hợp với cơ chế Fast-path giúp Node bắt kịp quãng thời gian chết 1-3 Epoch chỉ trong vòng 10-15 giây mà không cần phải phụ thuộc vào việc chép Snapshot thủ công.
