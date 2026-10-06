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

## 🔄 Các bước diễn ra tự động bên dưới:

1. **Lấy thông điệp cần ký (`mtn_getRegistrationMessage`)**:
   Lấy digest `REGISTER_ACCOUNT_V1:<user_address>:<cluster_key>` và `hashToSign`.
2. **Ký chữ ký ECDSA (`crypto.Sign`)**:
   Dùng private key secp256k1 của ví ký lên `hashToSign`.
3. **Gửi đăng ký (`mtn_registerAccount`)**:
   Gửi chữ ký lên node thực thi, nhận trạng thái ban đầu `PENDING`.
4. **Hệ thống tự động liên lạc với Parent Chain**:
   - `RegistrationRelay` trên node ký BLS và gửi `SendRegisterAccount` lên Parent Chain.
   - `RegistrationWorker` nhận Quorum f+1 từ Parent Chain và đẩy System Transaction cập nhật cờ `ParentRegistered = true`.
5. **Hoàn tất (`CONFIRMED`)**:
   Thăm dò `mtn_getRegistrationStatus` cho tới khi đạt `CONFIRMED`. Lúc này ví có thể tự do gửi mọi transaction (transfer, deploy, smart contract) trên chain con.
