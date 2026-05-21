Ran command: `./simple_test.sh`
Ran command: `./simple_test.sh`
Viewed main.go:513-555

[ignoring loop detection]

Không phải vậy bạn nhé. Bạn cần phân biệt rõ giữa **hai file khác nhau**:

1. **File checkpoint tạm `/tmp/pending_check_${NODE_ID}.json` (hoặc `pending_check.json`):**
   * Đây mới là file chứa trạng thái block mốc (Block A) để đối chiếu.
   * **Sau khi xác minh thành công (verify thành công), file này sẽ bị xóa hoàn toàn.** Vì vậy, khi chạy lại hoặc khởi động lại node, chương trình sẽ không dùng lại checkpoint cũ này nữa.

2. **File lịch sử báo cáo `history_records.json`:**
   * Đây chỉ là file **log kết quả (report)** dùng để lưu vết các lần check trước (ví dụ: vòng 1 thành công, vòng 2 thành công...).
   * **Công cụ tuyệt đối không đọc file này để làm dữ liệu xác minh.** Bạn có thể để nguyên hoặc xóa đi đều không ảnh hưởng đến logic check của node.

---

### 🚨 Phân tích lỗi thực tế vừa xảy ra trong log của bạn:
Trong lần chạy `./simple_test.sh` vừa rồi, hệ thống đã dừng lại vì phát hiện **lỗi rò rỉ trạng thái lịch sử thực tế của Blockchain** ở chế độ chạy liên tục:

* **Thời điểm Block A = 1 (Lúc vừa gửi giao dịch):**
  * Số dư thực tế lưu lại lúc đó: `1999999999999999999999978999999`
  * Nonce thực tế lúc đó: `2`
* **Thời điểm Block B = 6 (Sau khi chain chạy tiếp thêm 5 block):**
  * Khi ta hỏi Node: *"Hãy cho tôi biết Số dư và Nonce tại Block 1"*
  * Node lại trả về Số dư: `1999999999999999999999957999998` và Nonce: `3` (Đây là trạng thái mới của block sau, không phải của Block 1!).

**Kết luận:** Node đã bị **ghi đè/trôi trạng thái lịch sử** (có thể do cơ chế Pruning hoặc State DB không lưu giữ dữ liệu quá khứ của Block 1). Công cụ test-history đã phát hiện đúng lỗi nghiêm trọng này của blockchain và dừng hệ thống thành công.

---
### 📋 Tóm tắt thay đổi
- **Đã thay đổi:** Không thay đổi mã nguồn ở lượt này (Chỉ phân tích lỗi thu được từ log kiểm thử).
- **Blast radius:** N/A.
- **🐛 Nguyên nhân lỗi:** Phát hiện lỗi thực tế ở Blockchain Layer-4: Khi query trạng thái tại Block 1 ở thời điểm chain đạt Block 6, node trả về trạng thái mới hơn (lệch Balance và Nonce), chứng tỏ state lịch sử của Block 1 đã bị trôi/ghi đè.
- **Rủi ro tiềm ẩn:** Pruning/State DB của Blockchain không giữ được lịch sử State đúng nghĩa.
- **Lưu ý hiệu năng:** N/A.
---