# Ethereum Key Tool

Công cụ dòng lệnh (CLI) bằng Go giúp tạo mới địa chỉ ví Ethereum hoặc khôi phục thông tin khóa (Public Key & Ethereum Address) từ một Private Key đã có sẵn.

## 📌 Tính năng
1. **Tạo mới (Generate):** Sinh khóa ngẫu nhiên an toàn (ECDSA secp256k1) để xuất ra:
   - Private Key (Hex)
   - Public Key Uncompressed (Hex)
   - Public Key Compressed (Hex)
   - Địa chỉ ví Ethereum (Address dạng hex bắt đầu bằng `0x`)
2. **Khôi phục (Recover):** Nhập vào một Private Key Hex để lấy lại Public Key và Ethereum Address tương ứng.

## 🛠️ Yêu cầu hệ thống
- **Go:** Phiên bản 1.23.5 trở lên.
- **Go Modules:** Dự án sử dụng thư viện bảo mật từ thư viện Go-Ethereum (`github.com/ethereum/go-ethereum`).

---

## 🚀 Cách chạy và sử dụng

### 1. Di chuyển vào thư mục công cụ
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/tool/eth_key
```

### 2. Chạy trực tiếp bằng `go run`

#### 2.1. Tạo mới cặp khóa
```bash
go run main.go generate
# Hoặc lệnh viết tắt:
go run main.go gen
```

#### 2.2. Khôi phục từ Private Key
```bash
go run main.go recover <PRIVATE_KEY_HEX>
# Hoặc lệnh viết tắt:
go run main.go rec <PRIVATE_KEY_HEX>
```
*Lưu ý: `<PRIVATE_KEY_HEX>` có thể có hoặc không có tiền tố `0x` đều được.*

*Ví dụ:*
```bash
go run main.go recover 28f0ad246c39b9b5a32692e4f9364e29c3df3cdd6ca6d88fcb40e9dc6bc6c511
```

---

### 📦 3. Biên dịch thành file thực thi (Build)

Để chạy nhanh hơn hoặc phân phối mà không cần cài đặt Go trên môi trường đích, bạn có thể biên dịch công cụ thành file thực thi:

```bash
# Biên dịch ra file thực thi tên là eth_key_tool
go build -o eth_key_tool main.go
```

Sau khi biên dịch xong, sử dụng như sau:
```bash
# Tạo mới
./eth_key_tool generate

# Khôi phục khóa
./eth_key_tool recover <PRIVATE_KEY_HEX>
```

---

## 💡 Ví dụ kết quả đầu ra

```text
==================================================================================
🔑 ETHEREUM KEY DETAILS
==================================================================================
  Private Key (Hex):         0xc789b6d849c5b29a94e73c43b56973977a019ea02a9297f115e7d421b6ca2422
  Public Key (Uncompressed): 0x0451f284a3f15a78e5ee4933076ba5c5154500a559b82b9941bf6473e60937ca49aa9e87b79d98fa366a34d22cc07f56f3d61104706ba1468e920068afa7cfd268
  Public Key (Compressed):   0x0251f284a3f15a78e5ee4933076ba5c5154500a559b82b9941bf6473e60937ca49
  Ethereum Address:          0xf1e0F3c3a2b3e56AcA37702f88D17651841c5Fbb
==================================================================================
```
