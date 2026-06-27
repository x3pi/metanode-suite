# Hướng dẫn chạy Register BLS & Fund Wallets qua TCP

Công cụ này giúp bạn tự động tạo ví mới, đăng ký BLS public key và chuyển một lượng native token từ địa chỉ mặc định sang các ví vừa tạo.

## 1. Cấu hình (`config.json`)

Đảm bảo bạn đã điền các thông tin cần thiết vào `config.json` trước khi chạy:

- `public_key_bls`: Public key BLS cần đăng ký (Bắt buộc).
- `transfer_amount`: Số lượng native token muốn chuyển sang các ví mới (Tính theo định dạng Wei. Mặc định là `1000000000000000000` = 1 token).
- Các cấu hình kết nối TCP và Device Key mặc định (để hệ thống sử dụng địa chỉ này làm địa chỉ nguồn `fromAddress` chuyển tiền).

## 2. Lệnh chạy

Sử dụng cờ `-count` để chỉ định số lượng ví bạn muốn tạo. (Mặc định là `1`).

### Cập nhật cấu hình IP tự động:
Trước khi chạy, bạn nên cập nhật IP các node vào `config.json` để tool biết nơi kết nối:
```bash
cd ../../scripts/update-ip
bash update-ip.sh
cd ../../register_bls/tcp
```
Lệnh này sẽ lấy cấu hình 5 node hiện tại và cập nhật tự động vào `config.json`.

### Chạy trực tiếp qua mã nguồn (Go Run):
```bash
# Tạo và xử lý cho 1 ví (Gửi đều lên 5 node)
go run main.go

# Tạo và xử lý cho N ví (ví dụ: 100 ví, gửi đều lên 5 node)
go run main.go -count 100

# Tạo và xử lý cho 100 ví NHƯNG chỉ gửi lên 1 node duy nhất (node m0)
go run main.go -count 20000 -single -skip_fund -trace
# Chỉ chuyển tiền
go run main.go -count=4000 -native_only

```

### Build ra file thực thi (Tùy chọn):
Nếu muốn biên dịch ra file binary để chạy nhanh hơn:
```bash
# Build
go build -o register_bls main.go

# Chạy file binary
./register_bls -count 50
```

## 3. Kết quả đầu ra

Sau khi chạy xong, công cụ sẽ:
1. Tạo ra các ví và lưu Private Key & Address vào thư mục sinh tự động: `../../test_tps/gen_spam_keys/generated_keys.json`
2. Đăng ký thành công BLS key cho toàn bộ các ví đó thông qua Connection Pool TCP.
3. Chuyển tiền từ `fromAddress` sang các ví mới tạo.
