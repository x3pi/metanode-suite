# Terminal 1: Chạy Upload qua TCP (Dùng cấu hình .env.1)
go run main.go -envfile=".env.1" -tcp

# Terminal 2: Chạy Download (Dùng cấu hình .env.2)
go run main.go -envfile=".env.2" -download="MÃ_KEY"



-mode=http: (Mặc định trước đây) Chạy qua HTTP chuẩn của Ethereum (tạo ETH Transaction và gọi method uploadChunk thông thường).
-mode=tcp: Gửi Transaction qua giao thức TCP (sử dụng connection pool như ban nãy).
-mode=http-bls: (Chế độ bạn vừa yêu cầu) Tạo giao dịch và ký bằng BLS (khóa thiết bị/MetaNode), đóng gói vào Protobuf TransactionWithDeviceKey rồi gọi trực tiếp method

go run main.go -clients=5 -mode=http-bls -envfile=.env.1 -workers=5

# startClient đến startClient + clientsCount - 1
go run main.go -startClient=0 -clients=5 -download=981ef5da35b6f6ac56fde7a33fe36d7673a7d036210a6dfdd90edf6f8ddbbc49 -envfile=.env.1 -workers=5

#  public=true : cho phép người khác tải file
go run main.go -clients=5 -mode=http-bls -envfile=.env.1 -workers=5 -public=true