# Terminal 1: Chạy Upload qua TCP (Dùng cấu hình .env.1)
go run main.go -envfile=".env.1" -tcp

# Terminal 2: Chạy Download (Dùng cấu hình .env.2)
go run main.go -envfile=".env.2" -download="MÃ_KEY"



-mode=http: (Mặc định trước đây) Chạy qua HTTP chuẩn của Ethereum (tạo ETH Transaction và gọi method uploadChunk thông thường).
-mode=tcp: Gửi Transaction qua giao thức TCP (sử dụng connection pool như ban nãy).
-mode=http-bls: (Chế độ bạn vừa yêu cầu) Tạo giao dịch và ký bằng BLS (khóa thiết bị/MetaNode), đóng gói vào Protobuf TransactionWithDeviceKey rồi gọi trực tiếp method

go run . -envfile .env.1 -size 0.1 -workers 5 -rounds 3 -mode=http-bls
go run . -envfile .env.1 -size 0.01 -workers 1 -rounds 3 -mode=tcp

go run . -envfile .env.1 -download a7e5eadf4e5d0687a50e79ebe47a00745c9d482365821547a785b6483e7fb263 -workers 10 -rounds 1
