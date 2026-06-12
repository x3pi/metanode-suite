# Tài liệu Hướng dẫn CI Monitor Start Nodes

Công cụ này dùng để theo dõi (monitor) repository `metanode` trên GitHub 24/7. 
Mỗi khi có code mới được đẩy (push) lên nhánh `main`, công cụ sẽ tự động:
1. Kéo (Pull) code mới nhất về server.
2. Gọi lệnh Build và Cập nhật cluster (`deploy_systemd_cluster.sh --all`).
3. Khởi động lại các node với code mới **(Giữ nguyên toàn bộ data/database)**.
4. Gửi thông báo kết quả chi tiết qua Telegram.

## Các file thành phần

- `ci_monitor_start_nodes.py`: Chứa logic xử lý bằng Python (kết nối GitHub, Telegram, chạy lệnh).
- `ci_monitor_start_nodes.sh`: Script bash quản lý việc chạy ẩn công cụ dưới nền.
- `ci_monitor_start_nodes.md`: File tài liệu hướng dẫn này.

---

## Cách sử dụng

Để đảm bảo tool hoạt động 24/7 ngay cả khi bạn tắt máy tính hay đóng Terminal, hãy **luôn sử dụng qua file bash `.sh`**.

Đầu tiên, cấp quyền thực thi cho file quản lý nếu chưa có:
```bash
chmod +x ci_monitor_start_nodes.sh
```

### 1. Khởi động Monitor
```bash
./ci_monitor_start_nodes.sh start
```
*Lệnh này sẽ chạy file python dưới dạng `nohup` (chạy ngầm), lưu số Process ID (PID) lại để bạn dễ dàng quản lý.*

### 2. Dừng Monitor
```bash
./ci_monitor_start_nodes.sh stop
```
*Tự động tìm kiếm tiến trình và tắt một cách an toàn.*

### 3. Kiểm tra trạng thái
```bash
./ci_monitor_start_nodes.sh status
```
*Sẽ báo là `🟢 RUNNING` (kèm theo PID) hoặc `🔴 STOPPED`.*

### 4. Xem Logs theo thời gian thực
```bash
./ci_monitor_start_nodes.sh logs
```
*Xem trực tiếp những gì Python script đang in ra (log kiểm tra commit mỗi 10 giây). Nhấn `Ctrl + C` để thoát màn hình xem log (yên tâm, Monitor vẫn tiếp tục chạy).*

---

## Lưu ý về Logs khi Deploy
- Khi `Monitor` phát hiện code mới và gọi lệnh cập nhật cluster, toàn bộ logs build và deploy (kết quả của file `deploy_systemd_cluster.sh`) sẽ được lưu thành các file `.log` riêng biệt nằm trong thư mục `auto_update_logs/` (cùng vị trí với script).
- Hệ thống sẽ chỉ giữ lại **5 file logs gần nhất** để không làm đầy ổ cứng của bạn.
- Trong trường hợp Deploy bị lỗi, Monitor sẽ lấy **50 dòng log cuối cùng** gửi thẳng lên kênh Telegram để bạn chẩn đoán lỗi nhanh mà không cần login vào server.
