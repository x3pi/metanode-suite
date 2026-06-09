
# tạo ví đk bls
go run main.go -count=1



# Cơ bản: dùng admin key trong config.json để confirm
go run main.go -config=config-test.json -wallet-pk=e6b400585f8e1df3bca8302b7657249046d40c8ed92dba9a057287e4beca587a

# Chỉ định admin key riêng để confirm
go run main.go -config=config-test.json \
  -wallet-pk=<PRIVATE_KEY_VI> \
  -admin-pk=<PRIVATE_KEY_ADMIN>

# Không cần confirm (chỉ gửi setBlsPublicKey, bỏ qua confirmAccountWithoutSign)
go run main.go -config=config-test.json \
  -wallet-pk=<PRIVATE_KEY_VI> \
  -no-confirm
