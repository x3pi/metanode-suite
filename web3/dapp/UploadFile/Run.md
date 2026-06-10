## client web

```bash
cd client
npm install
npm run dev
```
## go server

```bash
cd server 
go run main.go
```
## python

```bash
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

## Sử dụng

```bash
python download_client.py
```

Client sẽ:
1. Kết nối đến Go socket server tại `localhost:9999`
2. Lắng nghe download requests
3. Khi nhận được request, tải file từ Rust servers qua QUIC
4. Lưu file vào thư mục `./downloads/`

## Cấu hình

Có thể thay đổi các biến trong file `download_client.py`:
- `SOCKET_HOST`, `SOCKET_PORT`: Địa chỉ Go socket server
- `RUST_SERVER_1_ADDR_QUIC`, `RUST_SERVER_2_ADDR_QUIC`: Địa chỉ Rust QUIC servers
- `OUTPUT_DIR`: Thư mục lưu file đã tải

