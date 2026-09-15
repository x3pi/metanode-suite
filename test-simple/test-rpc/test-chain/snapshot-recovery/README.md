# 📸 METANODE SNAPSHOT GENERATION & RESTORE ZERO-FORK RECOVERY TEST

Tài liệu mô tả chi tiết và ngắn gọn luồng hoạt động kiểm thử khả năng khôi phục node từ Snapshot Server (Ansible Restore) kết hợp kiểm chứng toàn vẹn dữ liệu Xapian DB và đối chiếu 100% Zero-Fork.

---

## 🎯 Mục tiêu bài test
1. Tự động phát hiện Snapshot Server (Node chuyên tạo Snapshot - thường là SyncOnly `node-4`).
2. Ở mỗi vòng lặp: Đảm bảo mạng sinh ra một bản **Snapshot MỚI** (Block > snapshot cũ), tuyệt đối không dùng lại snapshot cũ.
3. Sử dụng Ansible xóa sạch hoàn toàn dữ liệu local của một Validator Node đích, tải bản snapshot mới từ server và giải nén phục hồi dữ liệu.
4. Kiểm tra Node đích khởi động lại, bắt kịp (catch-up) các block mới qua P2P Sync và đạt `eth_consensusReady`.
5. Xác minh tính toàn vẹn dữ liệu **Xapian DB**: Node vừa restore phải đọc được chính xác dữ liệu cũ đã index từ trước snapshot (`Iphone 13 Pro UPDATED`) và tìm kiếm văn bản thành công.
6. Bơm giao dịch trực tiếp qua Node vừa khôi phục và kiểm chứng 100% Zero-Fork (Block Hash & StateRoot trùng khớp tuyệt đối trên toàn bộ các node).

---

## 🔄 Luồng hoạt động chi tiết từng bước (Phương án B: Deploy & Tích lũy liên tục)

```
[BẮT ĐẦU VÒNG N]
   │
   ├─► [BƯỚC 0] Quét mạng & Kiểm tra an toàn (Phase 0):
   │    ├── Phân loại Validator Nodes (0, 1, 2, 3) & SyncOnly Snapshot Server (Node 4)
   │    ├── Rào chắn an toàn: Cấm chỉ định xóa node snapshot server
   │    └── Pre-check Zero-Fork toàn mạng trước khi test
   │
   ├─► [BƯỚC 1] Nạp Xapian mở đầu & Đảm bảo có bản Snapshot MỚI:
   │    ├── 🚀 DEPLOY CONTRACT MỞ ĐẦU (Chạy full 6 bước Xapian)
   │    ├── 💾 Lưu contract vào .xapian_recovery_contract.json (Danh sách tích lũy)
   │    ├── Quét /api/snapshots tìm bản Snapshot mới (block_number > LAST_SNAPSHOT_BLOCK)
   │    ├── Nếu chưa có: Tự động bơm 15 TX định kỳ để đẩy chain sinh Snapshot mới
   │    └── 🚨 [BẮT BUỘC] Round >= 2 phải có snapshot mới hơn, cấm dùng lại snapshot cũ
   │
   ├─► [BƯỚC 2] Khôi phục Node đích bằng Ansible (Ansible Restore):
   │    ├── Tạm thời bật cờ /tmp/monitors_ignore_nodes (tránh monitor cảnh báo giả)
   │    └── Chạy ansible_deploy.sh: Dừng service, XÓA SẠCH data/ cũ, tải snapshot mới và giải nén
   │
   ├─► [BƯỚC 3] Chờ Node Online, Catch-up Sync & Consensus Ready:
   │    ├── 1. wait_node_online: Chờ RPC node đích phản hồi
   │    ├── 2. wait-sync-node: Chờ P2P Sync bắt kịp đỉnh chuỗi (lag <= 5 blocks)
   │    ├── 3. wait_node_consensus_ready: Chờ Rust consensus sẵn sàng xử lý block
   │    └── 4. 🔍 [VERIFY-NODE] Xác minh TOÀN BỘ danh sách contracts đã tích lũy trên Node đích vừa restore
   │         └── (Đọc doc 0 tất cả contracts cũ + test QuerySearch "iphone" trên contract mới nhất)
   │
   ├─► [BƯỚC 4] Bơm giao dịch & Deploy Contract mới qua Node vừa khôi phục (Phương án B):
   │    ├── 1. Gửi đợt giao dịch ETH trực tiếp vào RPC của Node đích vừa khôi phục
   │    ├── 2. 🚀 [DEPLOY XAPIAN MỚI QUA NODE ĐÍCH] Deploy contract Xapian mới toanh qua cổng RPC của Node đích:
   │    │    ├── Thực thi full 6 bước (Deploy ➔ Setup ➔ ReadBack ➔ Update ➔ QuerySearch ➔ View)
   │    │    └── 💾 Nạp thêm contract vào danh sách (oldest genesis + 99 most recent, max 100)
   │    └── 3. ✍️ [WRITE-DOC] Gửi TX ghi cập nhật bổ sung qua chính Node đích
   │
   └─► [BƯỚC 5] Kiểm chứng toàn diện cả cụm & Xác nhận 100% Zero-Fork:
        ├── 1. Bơm giao dịch kiểm chứng toàn cụm
        ├── 2. 🌐 [VERIFY-CLUSTER] Đối chiếu chéo TOÀN BỘ danh sách contracts trên 100% nodes
        ├── 3. verify_equal_height_and_zero_fork: Kiểm tra Block Hash & StateRoot trên mọi node
        └── 4. 🏆 Xác nhận 100% Zero-Fork ➔ KẾT THÚC VÒNG N
```

---

### 📋 Mô tả chi tiết từng bước:

#### 🔍 Bước 0: Quét mạng & Kiểm tra an toàn (Phase 0)
- **Quét mạng**: Đọc `config.json`, gọi `eth_blockNumber` phân loại `Validator Nodes` (tham gia vote: 0, 1, 2, 3) và `SyncOnly Nodes` (chuyên tạo snapshot: 4).
- **Dò tìm Snapshot URL**: Tự động thăm dò endpoint REST `http://<IP>:8604/api/snapshots`.
- **Rào chắn an toàn**: Nếu chỉ định `--target-node` vào node tạo snapshot $\rightarrow$ **chặn ngay lập tức** (node snapshot không được tự xóa chính nó).
- **Pre-check Zero-Fork**: Xác nhận toàn bộ node có cùng chiều cao block, cùng Block Hash và cùng StateRoot trước khi bắt đầu.

#### 📸 Bước 1: Nạp Xapian & Đảm bảo có Snapshot MỚI
- Gọi `xapian_tool --mode=setup --round=<round> --force-deploy`:
  - Mỗi round tự động deploy một hợp đồng `TestDbXapianV2` mới riêng biệt.
  - Thực thi trọn vẹn **chu trình kiểm thử 6 bước đầy đủ**:
    1. **Deploy Contract**: Gửi bytecode tạo hợp đồng mới trên blockchain.
    2. **runStep1_Setup**: Khởi tạo database Xapian `products` và index 3 documents mẫu (Iphone 13 Pro, Samsung Galaxy S22, Macbook Pro 14).
    3. **runStep2_ReadBack**: Đọc ngược lại doc 0 từ database qua Event `Read_Data`.
    4. **runStep3_UpdateDoc**: Cập nhật nội dung doc 0 thành `"Iphone 13 Pro UPDATED"`.
    5. **runStep5b_QuerySearch**: Bơm giao dịch tìm kiếm full-text với từ khóa `"iphone"`, xác thực Event `Search_Item` trả về đúng document đã cập nhật.
    6. **runStep5c_GetData_View**: Gọi `eth_call` xác thực struct `ProductData` hoàn chỉnh được lưu trong storage.
  - **Chính sách lưu trữ & đối chiếu tối đa 100 contracts**:
    - Tích lũy địa chỉ contract vào danh sách lịch sử trong file [.xapian_recovery_contract.json](file:///home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-chain/.xapian_recovery_contract.json).
    - Áp dụng thuật toán pruning tối ưu: Luôn giữ lại **contract cũ nhất (Round 1 / Genesis)** và **99 contracts mới nhất** gần đây (`oldest + 99 most recent`). Toàn bộ các contract này đều nằm trong bản snapshot tiếp theo được sinh ra.
    - Tại Bước 3 (`verify-node`) và Bước 5 (`verify-cluster`), toàn bộ danh sách contract này sẽ được duyệt kiểm tra tính toàn vẹn 100%.
- **Tìm snapshot mới nhất**: Dùng thuật toán `max(data, key=block_number)` truy vấn `/api/snapshots`.
- **Bơm TX kích block**: Nếu snapshot hiện tại chưa mới hơn mốc của vòng trước (`CUR_BLOCK <= LAST_SNAPSHOT_BLOCK`), tự động bơm 15 TX định kỳ (tối đa 40 đợt) để chuỗi hoàn thành epoch và sinh bản snapshot mới.
- 🚨 **Nguyên tắc bắt buộc (Round $\ge$ 2)**: Phải có snapshot mới hơn (`CUR_BLOCK > LAST_SNAPSHOT_BLOCK`), nếu không script sẽ báo lỗi nghiêm ngặt và dừng test, **tuyệt đối không dùng lại snapshot cũ**.

#### 🔄 Bước 2: Khôi phục Node đích bằng Ansible
- Tạm thời bỏ qua cảnh báo Telegram cho Node đích (`/tmp/monitors_ignore_nodes`).
- Thực thi:
  ```bash
  ansible_deploy.sh --only-node <TARGET> --restore-node <TARGET> --snapshot-url <URL>
  ```
- Ansible dừng service của node, xóa sạch thư mục dữ liệu `data/` cũ, tải file snapshot mới nhất từ server và giải nén vào thư mục `data/` (bao gồm cả database state và index `xapian_node/`).

#### ⏳ Bước 3: Chờ Online, Catch-up Sync & Sẵn sàng Đồng thuận
1. `wait_node_online`: Chờ tối đa 90s cho đến khi RPC phản hồi.
2. Xóa cờ ignore để bật lại giám sát.
3. `go run main.go -wait-sync-node <TARGET> -max-lag 5`: Chờ node đích tải các block còn thiếu từ mốc snapshot đến đỉnh chuỗi hiện tại qua P2P Sync.
4. `wait_node_consensus_ready`: Chờ RPC `eth_consensusReady` trả về `ready: true`, đảm bảo tầng consensus Rust đã sẵn sàng đề xuất/xử lý block.
5. 🔍 **Kiểm tra dữ liệu Xapian sau khi restore**:
   - Gọi `xapian_tool --mode=verify-node --target-node=<TARGET>`:
   - Duyệt và xác minh **toàn bộ danh sách contracts** đã tích lũy (tối đa 100 contracts, gồm contract cũ nhất và mới nhất): Dùng `eth_call` đọc doc 0 $\rightarrow$ Kiểm tra đúng `"Iphone 13 Pro"`, `"electronics"`, `"apple"`.
   - Với contract mới nhất: Gửi transaction tìm kiếm `QuerySearch("iphone")` $\rightarrow$ Xác minh event trả về đúng dữ liệu.
   - Chứng minh snapshot khôi phục đầy đủ cả thư mục dữ liệu Xapian C++ và state qua mọi round.

#### ⚡ Bước 4: Bơm giao dịch & Deploy Contract Xapian mới trực tiếp qua Node vừa khôi phục (Phương án B)
1. Gửi giao dịch ETH kiểm chứng trực tiếp vào RPC của chính Node vừa restore.
2. 🚀 **Deploy Contract Xapian mới toanh trực tiếp qua Node đích** (Phương án B):
   - Gọi `xapian_tool --mode=setup --target-node=<TARGET> --round=<round> --force-deploy`
   - Chạy full chu trình 6 bước khởi tạo và nạp vào danh sách lịch sử [.xapian_recovery_contract.json](file:///home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-chain/.xapian_recovery_contract.json).
   - Chứng minh node đích sau khi khôi phục từ snapshot có đầy đủ năng lực deploy hợp đồng và tạo database Xapian mới trên toàn mạng.
3. ✍️ Gửi giao dịch ghi Xapian `xapian_tool --mode=write-doc` qua Node vừa restore để chứng minh node có đầy đủ quyền đọc/ghi sau phục hồi.

#### 🏆 Bước 5: Kiểm chứng toàn cụm & 100% Zero-Fork
1. Gửi đợt giao dịch kiểm chứng qua toàn bộ cụm node (`-check-fork=true -require-all-alive=true`).
2. Gọi `xapian_tool --mode=verify-cluster`: Quét qua toàn bộ node trong mạng, đối chiếu chéo kết quả đọc dữ liệu của **toàn bộ danh sách contracts** đã lưu (tối đa 100 contracts, gồm contract cũ nhất và mới nhất), đảm bảo không có node nào bị lệch chỉ mục.
3. `verify_equal_height_and_zero_fork`: Chờ tất cả node đạt cùng block height, lấy `hash` và `stateRoot` của từng node đối chiếu chéo. Nếu có bất kỳ sự sai lệch nào $\rightarrow$ Báo lỗi `FORK DETECTED!`.
4. Nếu 100% trùng khớp $\rightarrow$ Kết luận vòng test thành công (`🏆 100% ZERO-FORK CONFIRMED`), nghỉ 10s trước khi sang vòng tiếp theo luân phiên node kế tiếp.

---

## 💻 Cách thực thi

```bash
# Chạy mặc định 1 vòng lặp (tự chọn Validator đầu tiên)
./run_snapshot_test.sh

# Chỉ định khôi phục đích danh Node 2
./run_snapshot_test.sh --target-node 2

# Chạy 3 vòng lặp luân phiên khôi phục các Validator khác nhau (mỗi vòng chờ snapshot mới)
./run_snapshot_test.sh --loop 3

# Tùy chỉnh URL Snapshot Server và số lượng TX kiểm chứng
./run_snapshot_test.sh --snapshot-url http://192.168.1.234:8604 --count 20 --loop 2
```
