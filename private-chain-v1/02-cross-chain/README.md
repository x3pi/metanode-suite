# 🌐 Cross-Chain Transfer & Smart Contract Call

Ví dụ mẫu trực quan minh họa cơ chế **Cross-Chain Communication** qua **Gateway Precompile Address (`0x0000000000000000000000000000000000001002`)** trên Metanode Network, bao gồm cả **chuyển tiền (Asset Transfer)** và **gọi Smart Contract (Contract Call)** xuyên chuỗi.

---

## 🏛️ Kiến trúc & Nguyên lý hoạt động

Mỗi node/chain tích hợp sẵn một Gateway Precompile tại địa chỉ `0x1002` với hàm:
```solidity
function outbound(
    uint256 destChainId,  // ID chain đích (hoặc Reserve Hub 991)
    address target,       // Địa chỉ ví hoặc Smart Contract đích
    bytes payload,        // Dữ liệu payload calldata (hoặc relay prefix MTNRELAY1:<DestChainID>)
    uint256 assetId,      // Loại tài sản (0 = Native MTN)
    uint256 value,        // Số lượng token chuyển
    uint256 tip,          // Phí thưởng cho Relayer
    uint256 gasFee,       // Phí thực thi EVM tại chain đích (đối với contract call)
    uint8 hopCount,       // Số chặng tối đa (mặc định 1)
    bool ordered          // Thực thi tuần tự (true/false)
) external returns (bytes32 messageId);
```

---

## 📦 Hai kịch bản chính trong ví dụ

### 1. Cross-Chain Native Asset Transfer (Chuyển tiền MTN)
- **Nguồn:** User tại **Chain A** (Chain ID: 101) gọi `outbound(...)` nộp 100 MTN.
- **Tiến trình:** Token tại Chain A bị khóa/burn -> Relayer phát hiện sự kiện -> Chuyển tiếp tới Reserve Hub/Chain B -> Mint token tương ứng cho Recipient tại **Chain B** (Chain ID: 102).

### 2. Cross-Chain Smart Contract Call (Gọi hàm hợp đồng)
- **Chuẩn bị:** Deploy contract `TestCounter` trên **Chain B** với hàm `increment()` và `getCount()`.
- **Thực thi:** User tại **Chain A** nộp lệnh `outbound(...)` mang payload `0xd09de08a` (`increment()`) hướng tới địa chỉ contract ở **Chain B**.
- **Kết quả:** Khi Relayer chuyển lệnh sang Chain B, hàm `increment()` được thực thi tự động và biến đếm tăng từ `0` lên `1`.

---

## 🚀 Cách chạy

```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/private-chain-v1/02-cross-chain

# Chạy trực tiếp (tự động đọc endpoint & keys từ ../config.json)
go run main.go

# Hoặc tùy biến thông qua flags:
go run main.go \
  -rpcA "http://192.168.1.233:8546" \
  -rpcB "http://192.168.1.233:8550" \
  -chainIDA 101 \
  -chainIDB 102 \
  -amount 100
```

---

## 📊 Đối soát kết quả
- Cả 2 lệnh `outbound` tại Chain A đều sinh `TxHash` thành công.
- Số dư ví Recipient trên Chain B tăng đúng lượng token đã chuyển.
- Gọi RPC `eth_call` vào contract `TestCounter` trên Chain B trả về `Counter = 1`.
