# Terminal 1
go run main.go -envfile=".env.1" -download="MÃ_KEY"

# Terminal 2
go run main.go -envfile=".env.2" -download="MÃ_KEY"

go run main.go -tcp
