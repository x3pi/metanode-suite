# 🌐 01-Client-Only-Transfer: Kiểm Thử Giao Dịch & Contract Call Xuyên Chuỗi

Kịch bản kiểm thử client độc lập (Pure Client) nộp giao dịch liên chuỗi từ **Chain Nguồn** sang **Chain Đích** thông qua hệ thống Gateway & Relayer.

---

## 🎯 Kịch Bản Thực Hiện (2 Phần)
1. **Chuyển Tiền Liên Chuỗi (Native Transfer)**:
   - Client nộp lệnh `outbound` tại Gateway Contract (`0x...1002`) trên Chain Nguồn.
   - Tiền được burn ở Chain Nguồn, Relayer gom chữ ký Quorum và mint sang Chain Đích.
2. **Gọi Smart Contract Xuyên Chuỗi (Cross-Chain Contract Call)**:
   - Deploy `TestCounter` contract trên Chain Đích.
   - Client nộp lệnh gọi hàm `increment()` từ Chain Nguồn ➔ Relayer chuyển tiếp và kích hoạt hàm tăng biến đếm trên Chain Đích.

---

## 🚀 Cách Chạy Nhanh

### 1. Chạy Mặc Định (Chain 101 ➔ Chain 102, Chuyển 500 MTN)
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/01-client-only-transfer
go run .
```

---

### 2. Chạy Nhanh Truyền Tham Số (Positional Args)
> **Cú pháp:** `go run . [Chain_Nguồn] [Chain_Đích] [Số_MTN]`

```bash
# Chuyển 100 MTN từ Chain 101 sang Chain 102:
go run . 101 102 100

# Chuyển 50 MTN ngược lại từ Chain 102 sang Chain 101:
go run . 102 101 50
```

---

### 3. Chạy Bằng Cờ Lệnh (CLI Flags)
> **Cú pháp:** `go run . -from <ID> -to <ID> -amount <Số_MTN>`

```bash
go run . -from 101 -to 102 -amount 200
```

---

## ⚙️ Các Cờ Tùy Chọn Nâng Cao (Override)

| Cờ (Flag) | Mặc định | Mô tả |
| :--- | :--- | :--- |
| `-from` / `-src` | `101` | Chain ID nguồn |
| `-to` / `-dst` | `102` | Chain ID đích |
| `-amount` / `-amt` | `500` | Số lượng MTN chuyển |
| `-config` | `../config.json` | Đường dẫn file cấu hình node & private keys |
| `-rpcA` / `-rpcB` | Tự động đọc config | Override địa chỉ JSON-RPC của Chain A / B |
| `-keyA` / `-keyB` | Tự động đọc config | Override Private Key của Sender / Recipient |

---

## ✅ Kết Quả Kỳ Vọng Khi Chạy Thành Công
- `🎉 BINGOOOO! TIỀN ĐÃ MINT TRÊN CHAIN 102 THÀNH CÔNG!`
- `🎉 BINGOOOO! SMART CONTRACT CHAIN 102 ĐÃ NHẬN LỆNH TỪ CHAIN 101 VÀ THỰC THI THÀNH CÔNG! (Counter = 1)`
- `✅ HOÀN TẤT KỊCH BẢN CLIENT (Chain 101 ➔ Chain 102)!`
