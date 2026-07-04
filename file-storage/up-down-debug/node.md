# Terminal 1: Chạy Upload qua TCP (Dùng cấu hình .env.1)
go run main.go -envfile=".env.1" -tcp

# Terminal 2: Chạy Download (Dùng cấu hình .env.2)
go run main.go -envfile=".env.2" -download="MÃ_KEY"



go run . -envfile .env.1 -size 0.1 -workers 20 -tcp=true -rounds 3


go run . -envfile .env.1 -download c6dc526e94939b209f99a3dabbef9189661d971f0ac3b8db7e96be2252a86eda
go run . -envfile .env.1 -download 90cff7c908458aa6059bbea5c31fed6e598fe4cc9e1d8b954b7bf3fe608facad