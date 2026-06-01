# Tài liệu Xử lý Lỗi & Cơ chế Phục hồi (Error Handling)
*Công cụ Benchmark: `tps_blast_cc`*

Tài liệu này phân loại các sự cố có thể xảy ra trong quá trình chạy automation test (nhiều round) và cơ chế xử lý tương ứng của công cụ.

---

## 🛑 1. Nhóm Lỗi FATAL (Ngắt toàn bộ tiến trình test)
Đây là các lỗi đặc biệt nghiêm trọng, cho thấy môi trường Blockchain hoặc kết nối mạng đã bị hỏng hoàn toàn. Nếu xảy ra, kịch bản test sẽ lập tức dừng lại (`os.Exit`) thay vì chạy các Round tiếp theo (tránh sinh ra dữ liệu rác).

| Loại Lỗi | Mô tả | Cơ chế xử lý |
| :--- | :--- | :--- |
| **Không thể kết nối (TCP)** | Không thể khởi tạo kết nối P2P tới Node mục tiêu sau 30 lần thử nghiệm (thường do Node bị crash hoặc kẹt port). | Dừng ngay lập tức. |
| **Đứt kết nối khi đang Blast** | Gửi hàng loạt giao dịch thất bại. | **Thử lại & Dừng:** Hệ thống sẽ tự động thử kết nối lại (Reconnect) tối đa **30 lần**, mỗi lần cách nhau **1 giây** (tổng timeout ~30s). Nếu thành công, gửi lại Batch. Nếu vẫn thất bại hoặc hết 30 lần, dừng ngay lập tức (tránh mất mát dữ liệu payload). |
| **Lỗi State/Phân nhánh (Fork)** | Công cụ `block_hash_checker` chạy ngầm phát hiện các node có state không đồng nhất hoặc fork. | Dừng ngay lập tức để giữ nguyên hiện trạng chain phục vụ debug (bảo toàn log). |
| **Fail toàn bộ Nonce** | Cả hệ thống RPC đều sập hoặc không lấy được bất kỳ nonce nào ở Round đầu tiên. | Dừng do không có data để build. |
| **Fail Fetch Receipt** | Không lấy được Receipt quá lâu ở bước xác minh cuối cùng. | Dừng toàn bộ để kiểm tra database. |

---

## ⚠️ 2. Nhóm Lỗi RECOVERABLE (Bỏ qua & Phục hồi tự động)
Đây là các sự cố mang tính thời điểm, gây tắc nghẽn hoặc lỗi cục bộ. Hệ thống sẽ **KHÔNG DỪNG BÀI TEST** mà kích hoạt cơ chế `break` (ngắt Round hiện tại) hoặc `continue` (bỏ qua dòng dữ liệu) để bài Test Automation có thể duy trì trơn tru suốt đêm.

| Loại Lỗi | Mô tả | Cơ chế khắc phục tự động |
| :--- | :--- | :--- |
| **Timeout Epoch (300s)** | Blockchain bị lag hoặc không sinh ra Epoch mới (vòng lặp polling bị kẹt). | 🔄 **Break & Tiếp tục:** Bỏ qua Round hiện tại, ghi nhận số TPS đã xử lý được, chốt kết quả và tự động dọn dẹp để chạy sang Round kế tiếp. |
| **Timeout Giao dịch (MaxWait)** | Có Block mới nhưng quá trình đưa TX vào block bị ngưng trệ vượt quá ngưỡng MaxWait. | 🔄 **Break & Tiếp tục:** Giống như lỗi Epoch, chốt kết quả sớm và bắt đầu Round mới. |
| **Lỗi Nonce cục bộ** | RPC từ chối trả về nonce cho một vài địa chỉ ví (do lag/đồng bộ chậm) trong tổng số hàng chục ngàn ví. | ⏭️ **Skip Account:** Cảnh báo `[RE-FETCH NONCE ERROR]`, bỏ qua các account lỗi, vẫn build giao dịch cho các account hợp lệ. |
| **Lỗi Build Transaction** | Ký số (signature) hoặc mã hóa bytecode thất bại cho một vài giao dịch. | ⏭️ **Skip TX:** Cảnh báo `Quá trình re-build giao dịch gặp x lỗi! Bỏ qua và tiếp tục...` và chỉ blast các TX thành công. |

---

## 💡 Lưu ý Vận Hành
- Nếu test chạy qua đêm và gặp lỗi **Nhóm 1**, bạn cần kiểm tra trực tiếp Log của Blockchain (`App.log` / `go-master-stdout.log`) vì 99% Node đã gặp Panic hoặc Deadlock nội bộ.
- Nếu gặp lỗi **Nhóm 2**, bài test vẫn sẽ sinh ra file báo cáo (`final_tps_summary.json`) để bạn đánh giá mức độ ổn định của mạng lưới dù có các Round bị Drop.
