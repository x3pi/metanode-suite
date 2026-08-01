# manage_genesis.py

```
--genesis-in (File Đầu Vào - Base/Source): Đây là file cấu hình gốc ban đầu của mạng lưới (chưa chứa các tài khoản rác/spam). Script sẽ ĐỌC file này để lấy cấu trúc cơ bản của mạng và danh sách các tài khoản genesis tối thiểu (như tài khoản của hệ thống, validator, node...). Nó đóng vai trò như một bản nháp sạch.

--genesis-out (File Đầu Ra - Destination/Target): Sau khi script tạo xong 100,000 keys mới trên RAM, nó sẽ nhét toàn bộ 100,000 keys này vào mảng alloc của cấu trúc vừa đọc từ in. Sau đó, nó sẽ LƯU (GHI) toàn bộ dữ liệu (file gốc + 100k account mới) ra file đường dẫn out. Đây chính là file bạn sẽ dùng để load thực tế khi chạy Node.


go run main.go --count 100000 --genesis-in /home/abc/nhat/con-chain-v2/metanode/execution/cmd/simple_chain/genesis-main.json --genesis-out /home/abc/nhat/con-chain-v2/metanode/deploy/systemd/genesis.json.example
```

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
./manage_genesis.py add \
  /home/abc/nhat/consensus-chain/mtn-2026/cmd/simple_chain/genesis.json \
  /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/gen_spam_keys/generated_keys.json
```bash