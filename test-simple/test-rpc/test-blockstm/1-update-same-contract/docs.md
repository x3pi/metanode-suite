# 📌 HƯỚNG DẪN CHẠY TEST 1-UPDATE-SAME-CONTRACT

## 1. Test EVM State Update (Chế độ mặc định)
go run main.go -config=../config.json -num=50
go run main.go -config=../config.json -num=5000
go run main.go -config=../config.json -num=10000 -rounds=8 -wait-method=block -multi

## 2. Test Xapian DB Shared Document Update (Thêm cờ -xapian)
go run main.go -config=../config.json -num=50 -xapian
go run main.go -config=../config.json -num=5000 -xapian
go run main.go -config=../config.json -num=10000 -rounds=8 -wait-method=block -multi -xapian
