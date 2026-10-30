# 🛠️ Metanode Account Gate Registration Tool (Go CLI)

Công cụ CLI bằng Golang để **đăng ký và onboarding ví mới hoàn toàn vào chain con (Execution Cluster)** thông qua cơ chế **Parent Chain Account Registration Gate**.

---

## 📌 Bối cảnh

Trên phiên bản chain mới:
- Chain chạy ở chế độ **`tx_signature_mode = "secp"`** (chuẩn Ethereum ECDSA secp256k1).
- **Không còn sử dụng chữ ký BLS cho tài khoản cá nhân** (không cần đăng ký contract `0xD844bb55` cũ nữa).
- Kích hoạt cơ chế **Account Registration Gate (`account_gate = "parent_registered"`)** để chống tấn công replay xuyên cụm.
- Mọi ví mới phải được đăng ký qua Node thực thi (Node thực thi và Parent Chain sẽ tự động chuyển tiếp và xác nhận).

---

## 🚀 Cách sử dụng

### 1. Đăng ký tự động 1 ví mới sinh ngẫu nhiên:
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/register_account
go run main.go -rpc=http://127.0.0.1:8646
```
*(Nếu node của bạn đang chạy ở port khác, ví dụ port `8747` hoặc IP khác thì truyền vào cờ `-rpc=http://192.168.1.234:8747`)*

### 2. Sinh và đăng ký hàng loạt 5 ví mới:
```bash
go run main.go -rpc=http://127.0.0.1:8646 -count=5 -out=my_wallets.json
```
Kết quả danh sách ví (địa chỉ, private key, trạng thái `CONFIRMED`) sẽ tự động được lưu vào file `my_wallets.json`.

### 3. Đăng ký cho 1 ví ĐÃ CÓ SẴN (thông qua Private Key):
```bash
go run main.go -rpc=http://127.0.0.1:8646 -wallet-pk=0x4f3edf983ac636a65a842ce7c78d9aa706d3b113bce9c46f30d7d21715b23b1d
```

### 4. Kiểm tra trạng thái Gate của một ví bất kỳ (không thực hiện đăng ký):
```bash
go run main.go -rpc=http://127.0.0.1:8646 -check=0x90F79bf6EB2c4f870365E785982E1f101E93b906
```

---

## ⚡ Đăng ký qua giao thức TCP Socket (Async Push Notification - Không Polling)

Khi sử dụng cờ `-tcp`, toàn bộ luồng đăng ký sẽ giao tiếp trực tiếp qua cổng TCP Socket của node. Node sẽ **chủ động bắn kết quả (Push Notification)** về lại socket khi L1 xác nhận, **loại bỏ hoàn toàn cơ chế Polling**:

### 1. Đăng ký ví mới qua TCP Socket:
```bash
go run main.go -tcp=192.168.1.223:4200
```

### 2. Sinh và đăng ký hàng loạt ví mới qua TCP:
```bash
go run main.go -tcp=127.0.0.1:8080 -count=5 -out=my_wallets_tcp.json
```

### 3. Đăng ký cho 1 ví có sẵn qua TCP:
```bash
go run main.go -tcp=127.0.0.1:8080 -wallet-pk=0x4f3edf983ac636a65a842ce7c78d9aa706d3b113bce9c46f30d7d21715b23b1d
```

### 4. Sử dụng file cấu hình TCP (`-tcp-config`):
```bash
go run main.go -tcp-config=../test-simple/test-tcp/caller-tcp/config-local.json
```

### 5. Kiểm tra trạng thái ví qua TCP:
```bash
go run main.go -tcp=127.0.0.1:8080 -check=0x90F79bf6EB2c4f870365E785982E1f101E93b906
```

---

## 💻 Luồng lập trình đăng ký qua TCP Socket (SDK Functions)

Để tích hợp luồng đăng ký tài khoản qua TCP vào các module hoặc ứng dụng khác, bạn chỉ cần sử dụng [Client](file:///home/abc/nhat/consensus-chain/metanode-suite/pkg/client-tcp/client_chain.go) (`pkg/client-tcp`) với các hàm sau:

```
[Client]                                                [Execution Node :4200]                   [Parent Chain L1]
   │                                                               │                                     │
   ├── (1) ChainGetRegistrationStatus(addr) ──────────────────────>│ (Kiểm tra xem đã đăng ký chưa)      │
   │<──────────────── pb.RegistrationResult ───────────────────────┤                                     │
   │                                                               │                                     │
   ├── (2) ChainGetRegistrationMessage(addr) ─────────────────────>│                                     │
   │<──────────────── 32-byte hashToSign ──────────────────────────┤                                     │
   │                                                               │                                     │
   │   [Ký ECDSA offline bằng Private Key]                         │                                     │
   │   sig = crypto.Sign(hashToSign, privKey)                      │                                     │
   │                                                               │                                     │
   ├── (3) ChainRegisterAccountWait(addr, sig, timeout) ──────────>│                                     │
   │<──────────────── pb.RegistrationResult (Ack: PENDING) ────────┤ (Lưu connection vào Tracker)        │
   │                                                               ├── Gửi Relay lên L1 ────────────────>│
   │                                                               │<── Xác nhận từ L1 (f+1 quorum) ─────┤
   │<════════════════ pb.RegistrationResult (PUSH: CONFIRMED) ═════╡ (Chủ động Push về Socket)           │
```

### Các hàm cần gọi trong Code Go:

#### 1. Khởi tạo Client TCP
```go
import client_tcp "tool-test/pkg/client-tcp"

// Khởi tạo client kết nối tới cổng TCP của Execution Node (ví dụ 192.168.1.223:4200)
client, err := client_tcp.NewClientWithConfig(clientConfig, "")
if err != nil {
    log.Fatal(err)
}
defer client.Stop()
```

#### 2. Kiểm tra trạng thái ví (Tùy chọn)
```go
// Gọi hàm: ChainGetRegistrationStatus
// TCP Command: GetRegistrationStatus -> Trả về: pb.RegistrationResult
status, err := client.ChainGetRegistrationStatus(userAddr)
if err == nil && status.Status == "CONFIRMED" {
    fmt.Println("Ví đã được đăng ký từ trước!")
    return
}
```

#### 3. Lấy 32-byte Hash thông điệp cần ký
```go
// Gọi hàm: ChainGetRegistrationMessage
// TCP Command: GetRegistrationMessage -> Trả về: 32 bytes Keccak256 hashToSign
hashToSign, err := client.ChainGetRegistrationMessage(userAddr)
if err != nil {
    return fmt.Errorf("lỗi lấy hash đăng ký: %w", err)
}
```

#### 4. Ký chữ ký ECDSA secp256k1 offline
```go
// Ký chữ ký 65-byte [R || S || V] từ Private Key của ví
sigBytes, err := crypto.Sign(hashToSign.Bytes(), privKey)
if err != nil {
    return fmt.Errorf("lỗi ký: %w", err)
}
```

#### 5. Gửi đăng ký & chờ kết quả Push (Không Polling)
```go
// Gọi hàm: ChainRegisterAccountWait
// TCP Command gửi: RegisterAccount (bọc pb.RegisterAccountRequest)
// TCP Response nhận: 
//   - Lập tức nhận RegisterAccountAck (pb.RegistrationResult: PENDING)
//   - Node tự động theo dõi L1 và chủ động PUSH RegistrationResult (pb.RegistrationResult: CONFIRMED)
result, err := client.ChainRegisterAccountWait(userAddr, sigBytes, 60*time.Second)
if err != nil {
    return fmt.Errorf("đăng ký thất bại: %w", err)
}

if result.Status == "CONFIRMED" {
    fmt.Printf("✅ Đăng ký thành công ví %s!\n", userAddr.Hex())
}
```

---

## 🔄 Các bước diễn ra tự động bên dưới node:

1. **Lấy thông điệp cần ký (`GetRegistrationMessage`)**:
   Lấy digest `REGISTER_ACCOUNT_V1:<user_address>:<cluster_key>` và Keccak256 `hashToSign`.
2. **Ký chữ ký ECDSA (`crypto.Sign`)**:
   Dùng private key secp256k1 của ví ký lên `hashToSign`.
3. **Gửi đăng ký (`RegisterAccount`)**:
   Gửi `pb.RegisterAccountRequest` lên node thực thi, nhận `RegisterAccountAck` trạng thái `PENDING`. Node lưu kết nối socket của client vào `RegistrationTracker`.
4. **Hệ thống tự động liên lạc với Parent Chain**:
   - `RegistrationRelay` trên node ký BLS và gửi `SendRegisterAccount` lên Parent Chain.
   - Khi Parent Chain xác nhận (f+1 quorum), node gọi `tracker.PushResult`.
5. **Node chủ động Push kết quả (`RegistrationResult`)**:
   Node gửi thẳng gói tin `command.RegistrationResult` chứa `pb.RegistrationResult{Status: "CONFIRMED"}` về socket đang mở của client. Client nhận được ngay lập tức mà không cần bất kỳ vòng lặp Polling nào. Lúc này ví đã được mở Gate và tự do gửi mọi transaction trên chain.
