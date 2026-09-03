# 👛 EIP-7702 Gas Sponsorship (Ví Trả Phí Hộ)

Ví dụ mẫu trực quan mô phỏng cơ chế **Account Abstraction theo chuẩn EIP-7702** (SetCode Transaction - `TxType = 0x04`), trong đó một bên thứ ba (Sponsor / Paymaster) đứng ra nộp giao dịch và thanh toán toàn bộ chi phí gas thay cho người dùng (User EOA).

---

## 🎯 Ý nghĩa & Ứng dụng thực tế

1. **Onboarding người dùng mới 0 vốn (Gasless Onboarding):** Người dùng tạo ví mới chưa có native token vẫn có thể tương tác blockchain ngay lập tức (chuyển token, mint NFT, chơi game, ký hợp đồng).
2. **Account Abstraction mà không cần Contract Factory tốn kém:** Biến ví EOA thường thành Smart Contract Wallet tạm thời hoặc vĩnh viễn nhờ gắn `delegation code` thông qua EIP-7702.
3. **Mô hình DApp tài trợ:** Dự án/DApp chịu phí gas cho user để tăng trải nghiệm người dùng (UX).

---

## ⚙️ Luồng hoạt động (Workflow)

```
+------------------------------------+
|  1. User (Authority EOA)          |
|     - Ký Authorization Tuple       |   (Ký offline hoàn toàn miễn phí)
|     - Chọn Delegate Contract Logic |
+-----------------+------------------+
                  |
                  | Gửi chữ ký AuthTuple
                  v
+-----------------+------------------+
|  2. Sponsor (Paymaster / Relayer)  |
|     - Đóng gói SetCodeTx (0x04)    |
|     - Ký giao dịch & Trả gas fee   |
+-----------------+------------------+
                  |
                  | eth_sendRawTransaction
                  v
+-----------------+------------------+
|  3. Blockchain Node (Mempool/EVM)  |
|     - Xác minh AuthTuple từ User   |
|     - Gắn Designator code vào User |
|     - Trừ gas từ Sponsor           |
+------------------------------------+
```

---

## 🚀 Cách chạy

```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/private-chain-v1/01-sponsored-tx-eip7702

# Chạy trực tiếp với cấu hình mặc định (tự động đọc ../config.json)
go run main.go

# Hoặc tùy biến thông số thông qua flags:
go run main.go \
  -rpc "http://192.168.1.233:8546" \
  -chainid 101 \
  -sponsor-key "<PRIVATE_KEY_SPONSOR>" \
  -user-key "<PRIVATE_KEY_USER>"
```

---

## 📊 Kết quả kỳ vọng
- Giao dịch được xác nhận với `TxType: 0x04` và `Status: 1 (SUCCESS)`.
- Tài khoản **Sponsor** bị trừ phí gas execution.
- Tài khoản **User** giữ nguyên số dư ban đầu 100%, đồng thời địa chỉ User được gắn delegation code `0xef0100 || <delegate_address>`.
