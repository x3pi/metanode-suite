Ran command: `git branch`
Viewed app_blockchain.go:553-593

Đúng vậy! Nhưng quyết định vào **SyncOnly** là do **Rust** quyết định, không phải Go. Đây là phân chia trách nhiệm:

```
Go (initBlockchain) ──────────────────────────────────
  ✅ Xác định điểm xuất phát (GEI, CommitIndex)
  ✅ Sẵn sàng nhận dữ liệu qua FFI socket
  (Go không biết mình bị lag bao nhiêu block)

Rust (consensus layer) ────────────────────────────────
  📡 Kết nối Peers trong mạng
  🔍 Hỏi: "Mày đang ở GEI bao nhiêu?" → Go trả lời GEI=239
  🔍 So sánh với mạng lưới: Mạng đang ở GEI=677
  ❗ Phát hiện gap = 677 - 239 = 438 commits còn thiếu
  🔒 Vào SyncOnly (khoá Proposals, không bỏ phiếu)
  🚀 fetch_commits() từ Peers → Bắn qua FFI sang Go
```

**Luồng Go nhận sau đó** nằm ở `processRustEpochData` trong `block_processor_network.go`. Tôi đã thêm log BOOT FLOW vào `app_blockchain.go` — nhưng để thấy **luồng catch-up đang chạy**, bạn cần xem log ở **2 chỗ** khi restart:

| Chỗ xem log | Thông tin |
|---|---|
| `App.log` của go-master | `[BOOT FLOW]` — Go đang ở bước nào khi init |
| `App.log` của go-master (sau đó) | `[PROCESSOR]` / `[BATCH-DRAIN]` — Rust đang bơm data sang |
| Log của Rust node | `[STARTUP-SYNC]` / `SyncOnly` / `Proposals LOCKED` |

Cụ thể: sau khi log `🎯 [BOOT FLOW] initBlockchain HOÀN TẤT` xuất hiện, nếu Rust phát hiện lag thì bạn sẽ thấy liên tiếp các log như:

```
[PROCESSOR] Starting loop to read from dataChan...
[BATCH-DRAIN] Drained 200 empty commits, highestGEI=439
[BATCH-DRAIN] Drained 150 empty commits, highestGEI=589
...
[FORK-SAFETY] Initialized from LastGlobalExecIndex: lastGEI=677
```

Bạn có muốn tôi thêm log vào `block_processor_network.go` để thấy rõ hơn quá trình Rust bơm data vào không?

---
### 📋 Tóm tắt thay đổi
- **Đã thay đổi:** Chỉ giải thích, không sửa code.
- **Blast radius:** Không có.
- **Rủi ro tiềm ẩn:** Không có.
- **Lưu ý:** SyncOnly là trạng thái của Rust, Go không chủ động vào/ra SyncOnly — Go chỉ nhận và xử lý data từ Rust qua FFI bất kể đang ở mode nào.
---