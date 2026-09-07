# 🎮 Demo Game Caro Xuyên Chuỗi (Cross-Chain Tic-Tac-Toe / Caro)

Demo trực quan hóa tính năng **EVM General Message Passing (GMP)** của Metanode Root Anchor Architecture thông qua trò chơi cờ Caro thời gian thực giữa 2 blockchain độc lập.

---

## 🏛️ Kiến Trúc Hoạt Động

```
[ Người chơi X (Chain 101) ] ──(Đánh ô 1,1)──► [ Private Chain 101 ]
                                                      │
                                                      ▼
                                       [ Public Chain 991 Root Anchor ]
                                          (BFT Quorum Certify Move)
                                                      │
                                                      ▼
[ Người chơi O (Chain 102) ] ◄──(Cập nhật ô 1,1)─ [ Private Chain 102 ]
```

1. **Người chơi X (Ví Genesis trên Chain 101):** Khóa tiền cược $50\text{ MTN}$ và đánh nước đi `(row, col)`.
2. **Người chơi O (Ví Genesis trên Chain 102):** Khóa tiền cược $50\text{ MTN}$ và đánh đáp trả.
3. **Public Chain 991 (Root Anchor):** Đóng vai trò trọng tài BFT, chứng thực chữ ký Quorum của từng nước đi và chuyển tiếp dữ liệu xuyên chuỗi.
4. **Giải ngân giải thưởng:** Khi một bên đạt 3 quân thẳng hàng, Smart Contract xác định người thắng và tự động giải ngân $100\text{ MTN}$ (gốc + tiền thưởng) về ví người thắng.

---

## 🚀 Cách Chạy Trận Đấu

Vào thư mục:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/04-cross-chain-caro-game
```

### 1. Chế độ Tự động mô phỏng ván đấu (Auto Simulation):
```bash
go run .
```

### 2. Chế độ Tương tác (Người chơi tự nhập nước đi từ bàn phím):
```bash
go run . --interactive
```
*(Bạn sẽ được yêu cầu nhập tọa độ hàng và cột, ví dụ `1 1`, `0 2`... để tự mình điều khiển Người chơi X trên Chain 101)*
