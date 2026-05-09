# Hướng dẫn sử dụng `register_validator`

# test

``` bash

address:
0x781E6EC6EBDCA11Be4B53865a34C0c7f10b6da6e

private key:
992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f
```

## Tham số dòng lệnh

| Flag | Mô tả | Ví dụ |
|---|---|---|
| `-mode` | Chọn chức năng: `register` (mặc định), `delegate`, `undelegate`, `deregister` | `undelegate` |
| `-key` | Private key hex (không có 0x) | `27c8f505bc...` |
| `-name` | Tên validator | `node-5` |
| `-primary` | Địa chỉ Primary (multiaddr format) | `/ip4/127.0.0.1/tcp/9005` |
| `-worker` | Địa chỉ Worker | `127.0.0.1:9005` |
| `-p2p` | Địa chỉ P2P (multiaddr format) | `/ip4/127.0.0.1/tcp/9005` |
| `-desc` | Mô tả | `"New Validator"` |
| `-commission` | Commission rate (1000 = 10%) | `1000` |
| `-min-delegation` | Stake tối thiểu (wei) | `0` |
| `-authority-key` | BLS G2 public key (base64, 96 bytes) | `kUYrYvf/...` |
| `-protocol-key` | Ed25519 public key (base64, 32 bytes) | `fN/BNA8P...` |
| `-network-key` | Ed25519 public key (base64, 32 bytes) | `jZ/kDNNP...` |
| `-hostname` | Hostname node | `node-5` |
| `-stake` | Lượng token stake (wei) | `1000000000000000000000` |
| `-dry-run` | Chỉ encode calldata, không gửi tx | (flag) |
| `-delegate-only` | Chỉ stake, bỏ qua đăng ký | (flag) |

## Yêu cầu: Tạo BLS & Ed25519 keys

**Đây là bước quan trọng nhất.** Keys phải được generate đúng format — **không thể dùng key ngẫu nhiên**:

```bash
# Vào thư mục consensus
cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode

# Generate keys cho N+1 nodes (đã có 4 nodes → generate 5)
cargo run -- generate --nodes 5

# Hoặc chỉ xem key hiện có
cat config/node_0_protocol_key.json
cat config/node_0_network_key.json
```

**Format key file** (Ed25519 - 64 bytes = private + public):

```json
{"private_key_bytes":[...64 bytes...],"scheme":"Ed25519"}
```

→ **Public key = 32 bytes CUỐI** của `private_key_bytes` → encode base64.

**Authority key (BLS)**: Được lấy từ field `authority_key` trong `committee.json` (đã là base64).

---

### Bước 1: Chuẩn bị keys cho validator mới

Cần có 3 keys hợp lệ từ Rust:

```bash
cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode

# Xem keys hiện có (node 0-3 đã được dùng)
cat config/committee.json | python3 -m json.tool

# Nếu cần generate thêm key cho node mới:
# cargo run -- generate --nodes 5  (thêm node 4)
```

Lấy keys từ `committee.json` (ví dụ dùng key của node-0 để test):

```
authority_key: kUYrYvf/fDUygF8+nIdNATAAlnQU3BZSD3aGuHNoAZQv3OJOIZKW+Uw+UbH/1LWCAlbyWnQra9vUSDJfFVIxlV4XlraaNkLsZSb3HMJJQK3qEc1L20Yqb5YM8uGRXvnB
protocol_key: fN/BNA8PFyjE3hclyjxnkYgjFlR6M27jpbocq7X847Y=
network_key:  jZ/kDNNPBsZXUD28FcxMLLZ+vCZCbEJoUvdB8zgRvug=
```

> ⚠️ Mỗi validator **phải có key độc lập**. Dùng chung key sẽ gây lỗi khi build committee.

### Bước 2: Chạy tool đăng ký

```bash


cd /home/abc/nhat/con-chain-v2/metanode/execution/cmd/tool/register_validator

go run main.go \
  -key           "992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f" \
  -name          "node-new-0" \
  -primary       "/ip4/127.0.0.1/tcp/9004" \
  -worker        "127.0.0.1:9004" \
  -p2p           "/ip4/127.0.0.1/tcp/9004" \
  -desc          "New validator registered by 0x781E" \
  -hostname      "node-0" \
  -commission    1000 \
  -stake         "1000000000000000000000" \
  -authority-key "kUYrYvf/fDUygF8+nIdNATAAlnQU3BZSD3aGuHNoAZQv3OJOIZKW+Uw+UbH/1LWCAlbyWnQra9vUSDJfFVIxlV4XlraaNkLsZSb3HMJJQK3qEc1L20Yqb5YM8uGRXvnB" \
  -protocol-key  "fN/BNA8PFyjE3hclyjxnkYgjFlR6M27jpbocq7X847Y=" \
  -network-key   "jZ/kDNNPBsZXUD28FcxMLLZ+vCZCbEJoUvdB8zgRvug="

go run main.go \
   -name "Clone-1" \
   -stake "1000000000000000000000" \
   -authority-key "kUYrYvf/fDUygF8+nIdNATAAlnQU3BZSD3aGuHNoAZQv3OJOIZKW+Uw+UbH/1LWCAlbyWnQra9vUSDJfFVIxlV4XlraaNkLsZSb3HMJJQK3qEc1L20Yqb5YM8uGRXvnB" \
   -protocol-key "fN/BNA8PFyjE3hclyjxnkYgjFlR6M27jpbocq7X847Y=" \
   -network-key "jZ/kDNNPBsZXUD28FcxMLLZ+vCZCbEJoUvdB8zgRvug=" \
   -key "08945a864012bab1b156108bb4ca98f09c2186a70fb41ad0435fff306fe1be65"

go run main.go \
   -name "Clone-2" \
   -stake "1000000000000000000000" \
   -authority-key "kUYrYvf/fDUygF8+nIdNATAAlnQU3BZSD3aGuHNoAZQv3OJOIZKW+Uw+UbH/1LWCAlbyWnQra9vUSDJfFVIxlV4XlraaNkLsZSb3HMJJQK3qEc1L20Yqb5YM8uGRXvnB" \
   -protocol-key "fN/BNA8PFyjE3hclyjxnkYgjFlR6M27jpbocq7X847Y=" \
   -network-key "jZ/kDNNPBsZXUD28FcxMLLZ+vCZCbEJoUvdB8zgRvug=" \
   -key "7b80653426df0e4fe5e1f9c4d16bcdf23389ded0d4b1e42bca2b5fcbc9a88daf"


go run main.go \
   -name "Clone-3" \
   -stake "1000000000000000000000" \
   -authority-key "kUYrYvf/fDUygF8+nIdNATAAlnQU3BZSD3aGuHNoAZQv3OJOIZKW+Uw+UbH/1LWCAlbyWnQra9vUSDJfFVIxlV4XlraaNkLsZSb3HMJJQK3qEc1L20Yqb5YM8uGRXvnB" \
   -protocol-key "fN/BNA8PFyjE3hclyjxnkYgjFlR6M27jpbocq7X847Y=" \
   -network-key "jZ/kDNNPBsZXUD28FcxMLLZ+vCZCbEJoUvdB8zgRvug=" \
   -key "7311d37235c053eb7d6c16ac4052c916f394f59534565d1194d13ed2d6b96cb0"


# Bước 1.1: Rút hết stake (vd: rút 1000 ETH)
go run main.go -mode undelegate \
  -key "992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f" \
  -stake "1000000000000000000000"

# Bước 1.2: Hủy đăng ký (gỡ validator khỏi danh sách)
go run main.go -mode deregister \
  -key "992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f"
  
```

### Bước 3 (nếu stake đã đủ): Chỉ delegate thêm

```bash
go run main.go \
  -delegate-only \
  -key   "YOUR_PRIVATE_KEY" \
  -stake "1000000000000000000000"
```

### Bước 5: Kiểm tra validator đã đăng ký chưa

```bash
cd /home/abc/nhat/con-chain-v2/metanode/execution/cmd/tool/check_validator_consensus
go run main.go -rpc http://localhost:8757 -addr 0x781E6EC6EBDCA11Be4B53865a34C0c7f10b6da6e
```

---

## Dry-run (kiểm tra calldata mà không gửi tx)

```bash
go run main.go \
  -dry-run \
  -key "YOUR_PRIVATE_KEY" \
  -name "test-node" \
  -authority-key "..." \
  -protocol-key "..." \
  -network-key "..."
```

---

## Luồng hoạt động sau khi đăng ký

```
1. registerValidator tx được ghi vào block
   → StakeDB lưu thông tin validator
   → Go gửi CommitteeChangedNotification qua /tmp/committee-notify.sock

2. delegate tx được ghi vào block
   → Validator có stake > 0

3. Chờ epoch transition (epoch_monitor poll mỗi 5s trên node 0)
   → Go báo epoch mới → Rust build committee mới
   → Committee mới bao gồm validator 0x781E...6da6e

4. Kiểm tra log Rust:
   grep "Built committee" /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_0/go-master/*.log
```

## Kịch bản: Thay thế Validator (1 người rời đi, 1 người mới vào)

Khi test kịch bản 1 validator rời khỏi mạng lưới (node dừng đồng thuận) và 1 validator mới đăng ký vào để thay thế, bạn làm theo các bước sau bằng flag `-mode`:

**1. Validator cũ rời đi (Undelegate -> Deregister)**
Đầu tiên, phải rút toàn bộ stake ra khỏi node, sau đó mới có thể hủy đăng ký.

```bash
# Bước 1.1: Rút hết stake (vd: rút 1000 ETH)
go run main.go -mode undelegate \
  -key "992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f" \
  -stake "1000000000000000000000"

# Bước 1.2: Hủy đăng ký (gỡ validator khỏi danh sách)
go run main.go -mode deregister \
  -key "992d48ebc2dbeb5fa65b53f5727a1c3f7c9d4730bab45d0fc6166c5481671d0f"
```

*(Ghi chú: Node vật lý của validator cũ có thể stop sau khi epoch transition tiếp theo diễn ra, vì khi đó validator cũ đã hoàn toàn bị xóa khỏi Rust committee).*

**2. Validator mới tham gia (Register)**
Chạy node mới, generate key mới, và đăng ký nó vào mạng lưới. Tool ở `mode=register` sẽ tự động xử lý cả hai việc: `registerValidator` và `delegate`.

```bash
go run main.go -mode register \
  -key "PRIVATE_KEY_MOI" \
  -name "node-replacement" \
  -stake "1000000000000000000000" \
  -authority-key "KEY_MOI..." \
  -protocol-key "KEY_MOI..." \
  -network-key "KEY_MOI..."
```

*(Đợi epoch transition diễn ra, Rust node sẽ nạp validator mới này vào committee thay cho người cũ).*

---

## Lỗi thường gặp

| Lỗi | Nguyên nhân | Giải pháp |
|---|---|---|
| `invalid BLS point` | AuthorityKey không phải điểm hợp lệ trên BLS12-381 | Generate key bằng `cargo run -- generate` |
| `insufficient funds` | Address thiếu balance | Chuyển token trước |
| `already registered` | Validator đã đăng ký | Dùng `-delegate-only` để chỉ thêm stake |
| `connection refused :8545` | Tool dùng port mặc định | Sửa `RPC_HTTP_URL` trong `main.go` thành `:8757` |
| `nonce too low` | Tx nonce lỗi | Đợi tx trước confirm rồi thử lại |
