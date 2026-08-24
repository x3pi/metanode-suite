# 🌐 Metanode Cross-Chain Test Suite Collection

Thư mục tập trung toàn bộ các bộ bài test & diễn tập liên chuỗi (Cross-Chain Root Anchor) của Metanode.

---

## 📂 Danh Sách Các Bộ Test (Test Suites)

| Thư mục / Suite | Mô tả | Lệnh chạy |
| :--- | :--- | :--- |
| **[`01-e2e-cross-chain-full/`](./01-e2e-cross-chain-full)** | **Toàn bộ chu trình E2E từ P0 ➔ P8**: Burn/Lock, Quorum Cert, Claim/Mint, EVM GMP Smart Contract Call, Chặn rút khống (10.7), Chống Replay (P5), Đa tài sản ERC-20 (P6), Cứu hộ Chain Death (P8). | `cd 01-e2e-cross-chain-full && go run .` |
| **[`02-p0-root-anchor/`](./02-p0-root-anchor)** | **Bộ kiểm thử 33 Kịch bản P0 Root Anchor**: Schema, Fuzzing Invariant (10.000 mutations), Governance 1-Chain-1-Vote, BLS12-381 PopVerify, StateRoot Checkpoints, và Adversarial Audit. | `cd 02-p0-root-anchor && go run .` |
| **[`03-chain-death-recovery/`](./03-chain-death-recovery)** | **Diễn tập Cứu Hộ Khẩn Cấp (P8 / T3.c)**: Cho phép tự tay tắt Chain 101 (`fuser -k 8546/tcp`) hoặc `--auto-kill` để kiểm chứng việc rút lại 100% tiền an toàn qua Public Chain 991. | `cd 03-chain-death-recovery && go run .` |

---

## 🚀 Hướng Dẫn Chạy Nhanh Từng Bài Test

### 1. Chạy bài test toàn diện E2E (P0 ➔ P8):
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/01-e2e-cross-chain-full
go run .
```

### 2. Chạy bài test 33 Kịch bản Root Anchor & PopVerify:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/02-p0-root-anchor
go run .
```

### 3. Diễn tập Tắt Chain 101 và Cứu Hộ Tài Sản:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/03-chain-death-recovery
go run main.go
```
*(Hoặc chạy tự động: `go run main.go --auto-kill --amount 1000`)*

---

## 🔄 Bật Lại 2 Private Chains Sau Khi Test
```bash
cd /home/abc/nhat/consensus-chain/metanode/deploy/systemd && bash setup_2_private_chains.sh
```
