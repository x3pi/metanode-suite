# 🚀 Hướng Dẫn Chạy Test Suite Block-STM

Bộ test kiểm thử toàn diện tính năng xử lý giao dịch song song (Block-STM), EVM State, Xapian DB, EIP-4844, EIP-7702 và tính tương thích trên cả **Public Chain** và **Private Chains**.

---

## 📌 1. Chạy Toàn Bộ Test Suite (`run_all_tests.sh`)

Tập lệnh sẽ chạy qua toàn bộ **33 bài test**, mỗi bài lặp lại **3 lần** (tổng cộng 99 lượt test).

```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm

# 🌐 Chạy trên Public Chain (Mặc định):
./run_all_tests.sh

# 🔒 Chạy trên Private Chain (Theo Chain ID):
./run_all_tests.sh --chain=101     # Chain A
./run_all_tests.sh --chain=102     # Chain B

# 🏷️ Chạy trên Private Chain (Theo Tên Chain):
./run_all_tests.sh --chain=chain_a
./run_all_tests.sh --chain=chain_b
```

---

## 📌 2. Chạy Từng Bài Test Đơn Lẻ

Di chuyển vào thư mục của bài test cần chạy (ví dụ `1-update-same-contract`):

```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/1-update-same-contract

# 🌐 Chạy trên Public Chain:
go run .

# 🔒 Chạy trên Private Chain (Dùng biến môi trường TARGET_CHAIN):
TARGET_CHAIN=101 go run .        # Theo Chain ID
TARGET_CHAIN=chain_a go run .    # Theo Tên Chain

# 🏷️ Hoặc dùng cờ --chain:
go run . --chain=101
```

---

## 📌 3. Cấu Hình Cố Định Qua File `config.json`

Mở file `config.json` và chỉnh sửa trường `"target_chain"` ở tầng root:

```json
{
  "target_chain": "101",
  "rpc_url": "http://192.168.1.233:10746",
  "chain_id": 100,
  "private_chains": {
    "chain_a": {
      "rpc_url": "http://192.168.1.233:8546",
      "chain_id": 101
    },
    "chain_b": {
      "rpc_url": "http://192.168.1.233:8547",
      "chain_id": 102
    }
  }
}
```

- Để `""`: Chạy trên Public Chain.
- Điền `"101"` hoặc `"chain_a"`: Tự động chạy trên Private Chain A.

---

## 🔑 4. Cơ Chế Tài Khoản & Private Keys

- Hệ thống tự động sử dụng **10 private keys genesis** có sẵn số dư lớn từ `config.json` cho cả Public Chain lẫn Private Chains.
- Không cần cấu hình lại khóa hay nạp tiền thủ công khi chuyển đổi qua lại giữa các chain.
