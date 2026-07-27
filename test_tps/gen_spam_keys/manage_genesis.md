# manage_genesis.py

`manage_genesis.py` là một công cụ tiện ích nhỏ viết bằng Python, được sử dụng để tự động thêm hoặc xóa hàng loạt accounts vào file genesis (thường dùng cho các bài test TPS hoặc spam txs).

## Cú pháp sử dụng

Script cần truyền vào 3 tham số: lệnh cần chạy (`add` hoặc `remove`), đường dẫn tới file genesis và đường dẫn tới file chứa danh sách key.

```bash
./manage_genesis.py <command> <genesis_file_path> <keys_file_path>
```

## Các lệnh hỗ trợ

### 1. Lệnh `add` (Thêm Accounts)
Đọc danh sách keys từ file `generated_keys` và đưa các địa chỉ (address) chưa tồn tại vào mảng `alloc` của `genesis-main.json`. 
- Nó sẽ tự động kiểm tra và **bỏ qua các địa chỉ bị trùng** (đã tồn tại sẵn).
- Mỗi account mới sẽ được tự động gán số dư mặc định là `2000000000000000000000000000000` wei.

**Ví dụ:**
```bash
./manage_genesis.py add ../../../metanode/deploy/systemd/genesis.json.example generated_keys.json.test
./manage_genesis.py add ../../../metanode/deploy/systemd/genesis.json generated_keys.json.test

```

### 2. Lệnh `remove` (Xóa Accounts)
Đọc danh sách keys từ file `generated_keys` và **xóa tất cả các địa chỉ tương ứng** khỏi mảng `alloc` của file `genesis-main.json`. 

**Ví dụ:**
```bash
./manage_genesis.py remove ../../../metanode/deploy/systemd/genesis.json generated_keys.json.test
```


# simple-chain

```bash
python3 /home/abc/nhat/consensus-chain/metanode-suite/test_tps/gen_spam_keys/manage_genesis.py add \
  /home/abc/nhat/mtn-simple-2025/cmd/simple_chain/genesis.json \
  /home/abc/nhat/consensus-chain/metanode-suite/test_tps/gen_spam_keys/generated_keys.json.test
```bash