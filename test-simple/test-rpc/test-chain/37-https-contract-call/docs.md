# Test 37: HTTPS TLS RPC Smart Contract Call

## 1. Mục đích bài test
Bài test này kiểm tra toàn diện khả năng giao tiếp và tương tác với blockchain Metanode qua giao thức bảo mật HTTPS RPC (TLS/SSL) được kết thúc (terminate) tại cổng Nginx reverse proxy (Port 443):
1. **Bắt tay TLS & Kết nối HTTPS RPC (`rpc.DialHTTPWithClient`)**: Xác thực chứng chỉ SSL/TLS, kết nối HTTP/2 hoặc HTTP/1.1 bảo mật.
2. **Truy vấn cơ bản qua HTTPS (`eth_blockNumber`, `eth_chainId`, `web3_clientVersion`)**: Xác thực thông số chain và trạng thái node.
3. **Đọc số dư & Nonce qua HTTPS (`eth_getBalance`, `eth_getTransactionCount`)**: Kiểm tra thông tin tài khoản người dùng qua kênh bảo mật.
4. **Triển khai Smart Contract (`TestCounter`) qua HTTPS**: Ký giao dịch và phát tán bằng `eth_sendRawTransaction`.
5. **Đọc dữ liệu Contract qua HTTPS (`eth_call`)**: Gọi hàm view/pure `getCount()` và xác thực giá trị khởi tạo bằng 0.
6. **Thực thi giao dịch Contract qua HTTPS (`eth_sendRawTransaction`)**: Gọi hàm `increment()` và chờ xác nhận block.
7. **Xác minh cập nhật trạng thái qua HTTPS**: Đọc lại `getCount()` xác nhận giá trị tăng lên 1.
8. **Kiểm tra tải đồng thời (Concurrent HTTPS RPC Requests)**: Gửi song song 10 truy vấn RPC qua kết nối HTTPS để kiểm tra khả năng tái sử dụng TLS connection pool và HTTP/2 multiplexing.

## 2. Kiến trúc & Cấu hình Nginx TLS
- **Nginx Reverse Proxy**: Lắng nghe tại cổng 443 với cấu hình chứng chỉ TLS (`/etc/nginx/tls/fullchain.pem` và `/etc/nginx/tls/privkey.pem`).
- **Proxy Upstream**: Chuyển tiếp lưu lượng RPC bảo mật từ client đến backend RPC node (`http://127.0.0.1:8747`).
- **Cấu hình Client**: Client tự động nhận diện `HTTPS_URL`, trường `https_url` trong `config.json` hoặc mặc định `https://localhost`.

## 3. Cách chạy test
### Chạy trực tiếp với go run:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/37-https-contract-call
go run main.go
```

### Chạy bằng go test:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/37-https-contract-call
go test -v -count=1 .
```

### Tuỳ chọn truyền URL HTTPS tùy chỉnh:
```bash
go run main.go --https=https://localhost
# hoặc qua biến môi trường:
HTTPS_URL=https://localhost go run main.go
```

