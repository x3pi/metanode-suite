# 🚀 Metanode Private Chain Kit (Standalone)

Bộ công cụ độc lập giúp khởi tạo và vận hành mạng **2 Metanode Private Chain (Chain 101 & Chain 102)**, đăng ký vào Gateway và chạy Cross-Chain Relayer để **chuyển tiền Native (MTN) & gọi Smart Contract xuyên chuỗi**.

---

## 📦 Cấu trúc Thư mục

```text
private_chain_kit/
├── bin/
│   ├── simple_chain         # Binary Node Blockchain (Execution & Core)
│   ├── metanode             # Binary Consensus Engine & Keytool
│   ├── bls_pubkey           # Helper công cụ trích xuất BLS G1 Public Key
│   ├── register_chains      # Tool tự động quản trị & đăng ký Chain lên Gateway (Root Anchor)
│   └── cross_chain_relayer  # Tool chuyển tiếp giao dịch liên chuỗi (Relayer)
├── inventory.yml            # File cấu hình trung tâm (khai báo các Chain 101, 102, 103...)
├── genesis.json             # Danh sách 133 ví cấp phát tiền ban đầu & Precompiles
├── gateway_register.json    # Cấu hình danh bạ đăng ký Gateway & Relayer (tự động cập nhật)
├── private_dev_keys.json    # Bộ khóa ví Developer test cho Chain 101, 102 (Sender/Recipient)
├── gen_single_chain.py      # Script tự động sinh cấu hình & bộ khóa cho Node
├── run_chains.sh            # Script tự động hóa toàn bộ: Start, Clean Data, Reset All, Auto-update Gateway
└── README.md                # Tài liệu hướng dẫn sử dụng
```

---

## ⚡ CÁCH 1: KHỞI CHẠY BẰNG SCRIPT TỰ ĐỘNG HÓA (`run_chains.sh`)

Script `./run_chains.sh` **tự động đọc cấu hình từ `inventory.yml`**, quản lý các chain (101, 102, 103...), tự động trích xuất khóa BLS và cập nhật `gateway_register.json`.



---

### 🌐 1. Khởi Chạy Đồng Loạt Toàn Bộ Chain (All Chains)

Khi không truyền cờ `--chain`, script tự động áp dụng lệnh cho **tất cả các chain đang bật trong `inventory.yml`**:

```bash

# 🔹 XÓA TRẮNG toàn bộ, sinh mới keys/genesis và tự cập nhật gateway_register.json:
./run_chains.sh --reset-all (xóa toàn bộ data)
# 🔹 Kiểm tra trạng thái hoạt động & block height của tất cả các chain:
./run_chains.sh --status

# 🔹 Khởi chạy tất cả các chain (101, 102, ...):
./run_chains.sh (giữa data chỉ khởi động lại)

## Thực hiện giao dịch xuyên chain

# 🔹 Đăng ký danh bạ các chain lên Gateway Contract (Root Anchor):
./run_chains.sh --register

# 🔹 Khởi chạy Cross-Chain Relayer Daemon: (chuyển tiếp các giao dịch xuyên chain)
./run_chains.sh --relayer


# 🔹 Dọn sạch database & logs của tất cả các chain, chạy lại từ block 0:
./run_chains.sh --clean-data
# 🔹 Dừng an toàn toàn bộ các Private Chain:
./run_chains.sh --stop
# 🔹 Khởi động lại toàn bộ các Private Chain:
./run_chains.sh --restart

```

---

### 🎯 2. Khởi Chạy 1 Chain Chỉ Định (Single Chain)

Sử dụng tùy chọn `--chain=ID` (hoặc viết tắt `-c ID`) để thực thi trên một chain cụ thể:

```bash
# 🔹 Khởi chạy riêng Chain 101 (giữ nguyên dữ liệu cũ, tự tạo config nếu chưa có):
./run_chains.sh --chain=101

# 🔹 Kiểm tra trạng thái hoạt động & block height riêng Chain 101:
./run_chains.sh --chain=101 --status

# 🔹 Dừng riêng Chain 101 (các chain khác vẫn chạy bình thường):
./run_chains.sh --chain=101 --stop

# 🔹 Khởi động lại riêng Chain 101:
./run_chains.sh --chain=101 --restart

# 🔹 Dọn database & logs (giữ nguyên keys), chạy lại từ block 0 cho riêng Chain 101:
./run_chains.sh --chain=101 --clean-data
```

#### 🔍 Lệnh kiểm tra xem 2 node đã khởi động thành công chưa:
Mở Terminal thứ 3 và chạy lệnh curl để kiểm tra RPC:
```bash
# Kiểm tra Chain 101:
curl -s http://127.0.0.1:8546 -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# Kiểm tra Chain 102:
curl -s http://127.0.0.1:8547 -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```
👉 *Nếu thành công, terminal sẽ trả về JSON chứa `"result":"0x..."` (ví dụ `"result":"0x0"` hoặc `"result":"0x1"`).*

### 📋 Cấu hình Chain động qua `inventory.yml`

File [inventory.yml](file:///home/abc/nhat/con-chain-v2/metanode/deploy/private_chain_kit/inventory.yml) cho phép khai báo danh sách bất kỳ Private Chain nào (101, 102, 103, 104...):
- Muốn thêm **Chain 103** hoặc **Chain 104**, bạn chỉ cần **bỏ dấu `#` (uncomment)** trong file `inventory.yml`.
- Khi chạy `./run_chains.sh`, script sẽ tự động nạp cấu hình (RPC port, offset, Submitter Key riêng) của các chain đó và tự động cập nhật danh bạ `gateway_register.json`.

---

## ⚙️ Yêu cầu Hệ thống & Lưu Ý Cấu Hình
- Hệ điều hành: Linux (Ubuntu 20.04/22.04 trở lên)
- Python: `python3`
- ⚠️ **Lưu ý xung đột cổng (Port Conflict):** Bộ kit này sử dụng các cổng `8546, 8547, 4210, 4220, 20210, 20220`. Hãy đảm bảo không chạy song song với cụm Ansible / Systemd Cluster (`/opt/metanode/...`).
- ⚠️ **Lưu ý Submitter Key (Chống Xung Đột Nonce):** Node 101 và Node 102 cần dùng ví riêng với Relayer Daemon khi gửi giao dịch lên Root Anchor (Chain 991). Script `./run_chains.sh` đã tự động cấu hình Wallet 12 cho Chain 101 và Wallet 13 cho Chain 102.

---

## 📖 CÁCH 2: HƯỚNG DẪN THỦ CÔNG TỪNG BƯỚC TỪ A-Z

---

### 🔹 BƯỚC 1: Sinh Cấu Hình Cho Cả 2 Chain (Chain 101 & Chain 102)

> ⚠️ **LƯU Ý KHI CHẠY LẠI TỪ ĐẦU (RESET):**  
> Nếu trước đó bạn đã từng chạy node và thư mục `./chain-101`, `./chain-102` đã có sẵn dữ liệu (`data/`), việc chạy đè script sẽ khiến bộ khóa mới bị lệch với cơ sở dữ liệu cũ. Hãy dừng node và xóa thư mục cũ trước:
> ```bash
> ./chain-101/stop_single_chain.sh 2>/dev/null || true
> ./chain-102/stop_single_chain.sh 2>/dev/null || true
> rm -rf ./chain-101 ./chain-102
> ```

Chạy script `gen_single_chain.py` để tạo cấu hình cho 2 Private Chain (lưu ý dùng submitter key riêng cho từng chain để không đụng Nonce với Relayer):

```bash
# 1. Sinh Private Chain 101 (RPC: 8546, Port Offset: 10, Submitter: Wallet 12)
python3 gen_single_chain.py \
  --chain-id 101 \
  --ip 127.0.0.1 \
  --rpc-port 8546 \
  --port-offset 10 \
  --root-anchor-rpc "http://192.168.1.234:10746" \
  --root-anchor-submitter-key "f5e6ba1cb14367c5264317dcb5f6e13f0d3cb0e3618e0a91f768570ab94b489c" \
  --output-dir ./chain-101

# 2. Sinh Private Chain 102 (RPC: 8547, Port Offset: 20, Submitter: Wallet 13)
python3 gen_single_chain.py \
  --chain-id 102 \
  --ip 127.0.0.1 \
  --rpc-port 8547 \
  --port-offset 20 \
  --root-anchor-rpc "http://192.168.1.234:10746" \
  --root-anchor-submitter-key "fc1ee6ee9341cbc12a7b214ba3a70955821fb6ae568a3bde8beb5681d782b713" \
  --output-dir ./chain-102
```

> 💡 **Tại sao Port Offset là 10 và 20?**
> Cụm Public Root Anchor đang sử dụng các port thấp (10200, 10201). Đặt `--port-offset 10` cho Chain 101 và `--port-offset 20` cho Chain 102 đảm bảo **100% không bao giờ bị đụng độ cổng mạng** (Consensus, P2P, PeerRPC).

#### 📋 Bảng Giải Thích Chi Tiết Tham Số Của `gen_single_chain.py`:

| Tham số | Giá trị | Ý nghĩa & Hướng dẫn |
| :--- | :--- | :--- |
| `--chain-id` | `101`, `102` | **EVM Chain ID** định danh riêng cho từng Private Chain. |
| `--ip` | `127.0.0.1` | **Địa chỉ IP máy chạy node**: Dùng `127.0.0.1` nếu chạy local, hoặc điền IP LAN (`192.168.1.x`) để máy ngoài kết nối. |
| `--rpc-port` | `8546`, `8547` | **Cổng JSON-RPC** của từng node (dùng cho Metamask, Web3 DApp kết nối). |
| `--port-offset` | `10`, `20` | **Độ lệch cổng nội bộ** (P2P, Consensus, Metrics): Tránh trùng cổng với cluster và giữa các chain. |
| `--root-anchor-rpc` | `http://192.168.1.234:10746` | **Địa chỉ RPC của Root Anchor (Gateway)** để liên kết mạng lưới đa chuỗi. |
| `--output-dir` | `./chain-101`, `./chain-102`| **Thư mục lưu cấu hình**: Chứa `genesis.json`, `config.json` và thư mục khóa `keys/`. |
| `--validators` | `1` | Số lượng validator node cho chain (mặc định: `1`). |
| `--alloc-balance`| `1000000` | Số lượng MTN nạp sẵn ban đầu cho các tài khoản dev (mặc định: 1,000,000 MTN). |

---

### 🔹 BƯỚC 2: Cập Nhật Khóa BLS Vào File `gateway_register.json`

File `gateway_register.json` đã có sẵn và được script `./run_chains.sh` tự động cập nhật khóa BLS của các chain. Nếu bạn muốn điền thủ công:

#### Vị trí lưu khóa BLS Private Key của từng chain:
Khóa BLS được tự động sinh ra tại **Bước 1** và được lưu trực tiếp trong thư mục của từng chain:

- **Khóa của Chain 101:**
  - **Đường dẫn file:** `chain-101/node-0/config.json`
  - **Trường cần copy:** Mở file `config.json`, tìm khối `"Databases"` -> copy chuỗi hex trong trường `"BLSPrivateKey"`.
  - *(Hoặc mở file `chain-101/node-0/keys/authority_key.json` -> copy trường `"private_key_hex"`)*.

- **Khóa của Chain 102:**
  - **Đường dẫn file:** `chain-102/node-0/config.json`
  - **Trường cần copy:** Mở file `config.json`, tìm khối `"Databases"` -> copy chuỗi hex trong trường `"BLSPrivateKey"`.
  - *(Hoặc mở file `chain-102/node-0/keys/authority_key.json` -> copy trường `"private_key_hex"`)*.

#### 3. Điền vào file `gateway_register.json`:
Mở file `gateway_register.json` và dán 2 key vừa lấy vào:
```json
{
  "root_anchor_rpc": "http://192.168.1.234:10746",
  "submitter_key": "d3d8157f2571153bcb664233f998a82b9b475fe509f92caf65ca2461bae7f1a9",
  "genesis_supply": "400000000000000000000000000",
  "per_chain_allocation": "100000000000000000000000000",
  "fund_genesis": true,
  "chains": [
    {
      "chain_id": 101,
      "rpc_url": "http://127.0.0.1:8546",
      "quorum_threshold": 6667,
      "validators": [
        {
          "name": "node-0",
          "node_id": 0,
          "bls_private_key": "<DÁN_KEY_BLS_CHAIN_101_VÀO_ĐÂY>",
          "stake": 1000
        }
      ]
    },
    {
      "chain_id": 102,
      "rpc_url": "http://127.0.0.1:8547",
      "quorum_threshold": 6667,
      "validators": [
        {
          "name": "node-0",
          "node_id": 0,
          "bls_private_key": "<DÁN_KEY_BLS_CHAIN_102_VÀO_ĐÂY>",
          "stake": 1000
        }
      ]
    }
  ]
}
```

#### 📋 Bảng Giải Thích Tham Số Trong `gateway_register.json`:
| Tham số | Ý nghĩa |
| :--- | :--- |
| `root_anchor_rpc` | Địa chỉ RPC của Root Anchor (Gateway trung tâm). |
| `submitter_key` | Khóa EVM Private Key gửi giao dịch nạp cọc đăng ký lên Root Anchor (đã điền sẵn key devnet). |
| `genesis_supply` | Tổng cung ban đầu phát hành cho quỹ Reserve (mặc định: 400,000,000 MTN). |
| `per_chain_allocation` | Hạn mức bảo chứng cấp phát cho mỗi Private Chain (mặc định: 100,000,000 MTN). |
| `fund_genesis` | `true`: Tự động nạp hạn mức bảo chứng để cho phép chuyển tiền xuyên chuỗi ngay lập tức. |
| `chain_id` | ID của Private Chain (101, 102). |
| `rpc_url` | Địa chỉ RPC của Private Chain (`8546` cho 101, `8547` cho 102). |
| `bls_private_key` | Khóa BLS Private Key lấy từ `config.json` ở trên. |
| `stake` | Số lượng MTN stake bảo chứng cho validator (mặc định: `1000`). |

---

### 🔹 BƯỚC 3: Khởi Động 2 Private Chain Node

Mở 2 cửa sổ Terminal riêng biệt để chạy 2 node:

- **Terminal 1 (Chạy Chain 101):**
  ```bash
  # Chạy trực tiếp (xem log foreground):
  ./bin/simple_chain -config ./chain-101/node-0/config.json
  # Hoặc chạy nền (background):
  ./chain-101/start_single_chain.sh
  ```
  *(RPC mở tại: `http://127.0.0.1:8546`, Dừng nền: `./chain-101/stop_single_chain.sh`)*

- **Terminal 2 (Chạy Chain 102):**
  ```bash
  # Chạy trực tiếp (xem log foreground):
  ./bin/simple_chain -config ./chain-102/node-0/config.json
  # Hoặc chạy nền (background):
  ./chain-102/start_single_chain.sh
  ```
  *(RPC mở tại: `http://127.0.0.1:8547`, Dừng nền: `./chain-102/stop_single_chain.sh`)*


---

### 🔹 BƯỚC 4: Đăng Ký Cả 2 Chain Lên Gateway

Mở Terminal thứ 3 và chạy lệnh đăng ký:
```bash
./bin/register_chains --config gateway_register.json
```
👉 Tool sẽ tự động ký BLS Proof-of-Possession (PoP) và đăng ký danh bạ 2 chain lên Gateway Contract của Root Anchor.

---

### 🔹 BƯỚC 5: Khởi Động Cross-Chain Relayer

Chạy tiến trình Relayer để tự động bắt và chuyển tiếp các giao dịch nạp/rút tiền và gọi contract xuyên chuỗi:
```bash
# Chạy trực tiếp:
./bin/cross_chain_relayer --config gateway_register.json
```

Relayer sẽ tự động theo dõi và xử lý cả **6 luồng liên chuỗi**:
- `Chain 101 ⇄ Chain 102` (Chuyển tiền & gọi contract trực tiếp giữa 2 Private Chain)
- `Chain 101 ⇄ Root Anchor (991)`
- `Chain 102 ⇄ Root Anchor (991)`

---

## ⚡ HƯỚNG DẪN THỰC THI GIAO DỊCH XUYÊN CHUỖI (CROSS-CHAIN)

Địa chỉ Gateway Contract trên tất cả các chain: **`0x0000000000000000000000000000000000001002`**

### 1️⃣ Chuyển Native Token (MTN) Xuyên Chuỗi:
Gửi giao dịch gọi hàm `deposit` vào Gateway Contract:
- **Từ Chain 101 sang Chain 102:**
  - Gửi transaction vào RPC `http://127.0.0.1:8546` (Chain 101) tới địa chỉ `0x...1002`.
  - Hàm gọi: `deposit(uint256 toChainId, address to, uint256 amount)`
    - `toChainId`: `102`
    - `to`: Địa chỉ ví nhận trên Chain 102
    - `amount` & `value`: Số lượng wei muốn chuyển.
  - 👉 **Relayer** sẽ tự động bắt sự kiện `Deposit`, xác minh Merkle proof và đúc/giải ngân tiền vào ví nhận trên Chain 102!

### 2️⃣ Gọi Smart Contract Xuyên Chuỗi:
- Gọi hàm `sendMessage(uint256 toChainId, address targetContract, bytes payload)` trên Gateway `0x...1002` tại Chain nguồn (Chain 101).
- 👉 **Relayer** sẽ chuyển tiếp payload sang Chain đích (Chain 102) và tự động thực thi hàm trên `targetContract`.

---

## 🔍 CÁC LỆNH KIỂM TRA & TRA CỨU (VERIFICATION)

Phần này dùng để kiểm tra lại trạng thái sau khi đã vận hành:

### 1️⃣ Kiểm tra Danh bạ Chain (`query-registry`)
Kiểm tra xem 2 chain đã được ghi nhận trên Gateway chưa:
```bash
./bin/register_chains --config gateway_register.json -action query-registry -chains "101,102"
```
👉 *Kết quả mong đợi:* Hiển thị `✅ Đã đăng ký (Epoch: 0, Committee: 1 validator(s))` cho cả Chain 101 và Chain 102.

### 2️⃣ Tra cứu Hạn mức Tiền cọc (`query-alloc`)
Xem số dư hạn mức bảo chứng (Custodial Allocation) còn lại của từng chain:
```bash
./bin/register_chains --config gateway_register.json -action query-alloc -chains "991,101,102"
```

### 3️⃣ Chuyển Hạn mức Tiền cọc Giữa 2 Chain (`transfer-alloc`)
Trích chuyển hạn mức từ Chain 101 sang Chain 102:
```bash
./bin/register_chains --config gateway_register.json -action transfer-alloc -from-chain 101 -to-chain 102 -amount-mtn 10000000
```

### 4️⃣ Làm Sạch Dữ Liệu & Reset Node (Clean Data / Reset All)

Tùy theo nhu cầu kiểm thử, bạn có 2 cách reset dữ liệu:

- **Cách 1: Reset về Block 0 (Giữ nguyên Bộ Khóa & Cấu hình):**  
  Xóa database cũ để node chạy lại từ đầu với cùng bộ key ban đầu (không cần phải cập nhật lại file `gateway_register.json`):
  ```bash
  # 1. Dừng node
  ./chain-101/stop_single_chain.sh
  ./chain-102/stop_single_chain.sh

  # 2. Xóa sạch DB và logs cũ
  rm -rf ./chain-101/node-*/data ./chain-101/node-*/logs
  rm -rf ./chain-102/node-*/data ./chain-102/node-*/logs

  # 3. Khởi động lại
  ./chain-101/start_single_chain.sh
  ./chain-102/start_single_chain.sh
  ```

- **Cách 2: Reset Hoàn Toàn (Xóa trắng & Sinh lại Key mới từ Bước 1):**  
  Dùng khi muốn tạo lại toàn bộ chuỗi mới tinh từ con số 0:
  ```bash
  # Dừng và xóa toàn bộ thư mục
  ./chain-101/stop_single_chain.sh 2>/dev/null || true
  ./chain-102/stop_single_chain.sh 2>/dev/null || true
  rm -rf ./chain-101 ./chain-102
  # Sau đó bắt đầu lại từ BƯỚC 1 để sinh cấu hình và bộ khóa mới!
  ```

### 🛑 Dừng Node & Relayer
- **Dừng Node:** Nhấn `Ctrl + C` trên terminal chạy node hoặc chạy `./chain-101/stop_single_chain.sh` và `./chain-102/stop_single_chain.sh`.
- **Dừng Relayer Tmux:** `tmux kill-session -t relayer`.

