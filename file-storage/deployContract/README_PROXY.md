# Proxy Pattern Deployment

## Tổng quan

File `main.go` đã được cập nhật để triển khai **Proxy Pattern** cho smart contract, cho phép nâng cấp contract trong tương lai mà không cần thay đổi địa chỉ.

## Kiến trúc

```
┌─────────────────┐
│  User/Client    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   FileProxy     │  ← Địa chỉ cố định (không đổi)
│  (Proxy)        │
└────────┬────────┘
         │ delegatecall
         ▼
┌─────────────────┐
│ Files Contract  │  ← Implementation (có thể nâng cấp)
│(Implementation) │
└─────────────────┘
```

## Quy trình triển khai

### Bước 1: Deploy Implementation Contract
- Deploy contract **Files** (chứa logic thực thi)
- Lưu địa chỉ implementation

### Bước 2: Deploy Proxy Contract
- Deploy contract **FileProxy** với:
  - `_implementation`: Địa chỉ của Files contract
  - `_data`: `0x` (empty bytes)

### Bước 3: Initialize Contract
- Gọi hàm `initialize()` thông qua proxy address
- Thiết lập owner và cấu hình ban đầu

### Bước 4-5: Configuration
- Set Rust Server Addresses
- Add Storage Servers

## Cách sử dụng

### 1. Chuẩn bị file .env

```bash
RPC_URL=http://192.168.1.234:8545
PRIVATE_KEY=your_private_key_here
IP_RUST_STORAGE=http://192.168.1.1:8080,http://192.168.1.2:8080
STORAGE_SERVER_ADDRESS=0x1234...,0x5678...
```

### 2. Chạy deployment

```bash
cd /home/abc/nhat/mtn-simple-2025/hybrid-storage-demo/deployContract
go run main.go
```

### 3. Kết quả

File `deployment_YYYYMMDD_HHMMSS.json` sẽ được tạo với nội dung:

```json
{
  "implementationContract": "0x...",
  "proxyContract": "0x...",
  "deployer": "0x...",
  "rpcUrl": "http://...",
  "rustServerIPs": [...],
  "storageServerAddresses": [...],
  "timestamp": "2025-12-11T..."
}
```

## Lợi ích của Proxy Pattern

### 1. Nâng cấp contract
- Có thể deploy implementation mới
- Cập nhật proxy để trỏ đến implementation mới
- Giữ nguyên địa chỉ proxy → Không cần thay đổi client

### 2. Bảo toàn state
- Data được lưu trong proxy storage
- Khi nâng cấp chỉ thay đổi logic, không mất data

### 3. Tiết kiệm gas
- Chỉ cần upgrade implementation khi cần
- Không cần migrate data

## Cấu trúc Code

### Hàm chính

1. **`deployFilesImplementation()`**
   - Deploy Files contract (implementation)
   - Trả về địa chỉ implementation

2. **`deployFileProxy()`**
   - Deploy FileProxy contract
   - Nhận implementation address làm tham số
   - Constructor: `(address _implementation, bytes memory _data)`
   - Trả về địa chỉ proxy

3. **`initializeContract()`**
   - Gọi hàm `initialize()` thông qua proxy
   - Khởi tạo contract state

4. **`setRustServerAddresses()`**
   - Set danh sách Rust storage server IPs

5. **`addStorageServer()`**
   - Thêm storage server address

## Lưu ý quan trọng

### Nonce Management
- Nonce được tự động tăng sau mỗi transaction:
  - Nonce 1: Deploy Implementation
  - Nonce 2: Deploy Proxy
  - Nonce 3: Initialize
  - Nonce 4+: Configuration

### Yêu cầu Nonce
```go
if nonce != 1 {
    return nil, fmt.Errorf("yêu cầu deploy thất bại: nonce phải là 1 (đang là %d)", nonce)
}
```

### Tương tác với Contract
**Luôn sử dụng địa chỉ Proxy** khi tương tác với contract:
```go
// ✅ Đúng
filesContract := bind.NewBoundContract(proxyAddress, parsedABI, client, client, client)

// ❌ Sai
filesContract := bind.NewBoundContract(implementationAddress, parsedABI, client, client, client)
```

## Upgrade Contract (Tương lai)

Khi cần nâng cấp contract:

1. Deploy implementation mới
2. Gọi hàm `upgradeToAndCall()` trên proxy với địa chỉ implementation mới
3. Tất cả giao dịch tiếp tục sử dụng proxy address cũ

## Files cần thiết

- `main.go` - Code deployment
- `fileAbi.json` - ABI của Files contract
- `byteCode/byteCode.json` - Bytecode của cả implementation và proxy
- `.env` - Cấu hình deployment

## Cấu trúc byteCode.json

```json
{
  "file": "0x608060405...",      // Bytecode của Files implementation
  "fileProxy": "0x608060405..."  // Bytecode của FileProxy
}
```
