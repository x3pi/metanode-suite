# 📌 HƯỚNG DẪN CHẠY TEST 1-UPDATE-SAME-CONTRACT

parallel: thực hiện không sung đột

## 1. Test EVM State Update (Chế độ mặc định)
go run main.go -config=../config.json -num=50
go run main.go -config=../config.json -num=5000 -parallel
go run main.go -config=../config.json -num=10000 -rounds=8 -wait-method=block
go run main.go -config=../config.json -num=10000 -rounds=8 -wait-method=block -multi

## 2. Test Xapian DB Shared Document Update (Thêm cờ -xapian)
go run main.go -config=../config.json -num=50 -xapian
go run main.go -config=../config.json -num=5000 -xapian -parallel
go run main.go -config=../config.json -num=10000 -rounds=8 -wait-method=block
go run main.go -config=../config.json -num=10000 -rounds=32 -wait-method=block -multi -xapian




go run main.go -config=../config.json -multi -xapian -check="0xaB81d10D403e23552b0F6eadFd96F745409275AD"