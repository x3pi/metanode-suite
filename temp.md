Ran command: `go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.31.0`
Viewed ci_monitor.py:130-166

Lệnh `go install` đã cài đặt thành công, nhưng hệ thống vẫn báo `Command not found` là do Go lưu các file thực thi (binary) vào thư mục `~/go/bin`, và thư mục này **chưa có trong biến môi trường PATH** của bạn.

Để shell (terminal) có thể nhận diện được lệnh `protoc-gen-go`, bạn cần chạy lệnh sau để thêm `~/go/bin` vào PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Sau khi chạy xong, hãy kiểm tra lại bằng lệnh:
```bash
protoc-gen-go --version
```
Nó sẽ in ra đúng `protoc-gen-go v1.31.0`.

*(Mẹo: Để không phải gõ lại lệnh export này mỗi khi mở terminal mới, bạn hãy lưu nó thẳng vào file cấu hình bash bằng lệnh sau:)*
```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
```

Sau khi cấu hình xong PATH, quá trình chạy `./auto_test.sh` sẽ vượt qua được lỗi build Protobuf!

---
### 📋 Tóm tắt thay đổi
- **Đã thay đổi:** Hướng dẫn cập nhật biến môi trường `PATH` để nhận diện thư mục `bin` của Go. Không thay đổi code.
- **Blast radius:** Môi trường terminal/shell của user (Build environment).
- **🐛 Nguyên nhân lỗi:** Khi cài đặt qua lệnh `go install`, công cụ `protoc-gen-go` được lưu mặc định tại `$(go env GOPATH)/bin` (thường là `~/go/bin`). Tuy nhiên, đường dẫn này chưa được khai báo trong biến `PATH` của Linux, khiến OS không thể định vị được file chạy khi được gọi dưới dạng global command.
- **Rủi ro tiềm ẩn:** Không có. Khai báo `GOPATH/bin` vào `PATH` là bước thiết lập môi trường (environment setup) bắt buộc và an toàn tuyệt đối khi phát triển bằng ngôn ngữ Go.
- **Lưu ý hiệu năng:** Không liên quan.
---