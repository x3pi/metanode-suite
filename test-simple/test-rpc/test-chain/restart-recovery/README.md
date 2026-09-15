# 🌐 METANODE CHAOS ROLLING RESTART & ZERO-FORK RECOVERY TEST

Tài liệu mô tả chi tiết và ngắn gọn luồng hoạt động kiểm thử khả năng chịu lỗi khi tắt/bật node (Chaos Rolling Restart & Full Cluster Restart) kết hợp kiểm chứng toàn vẹn dữ liệu Xapian DB và chuẩn 100% Zero-Fork.

---

## 🎯 Mục tiêu bài test
1. Chứng minh mạng Metanode chịu lỗi an toàn khi tắt/bật luân phiên từng Validator Node (Rolling Restart) mà không làm ngắt quãng chuỗi.
2. Kiểm tra node sau khi khởi động lại (`restart`) đồng bộ đuổi kịp (catch-up) đỉnh chuỗi và sẵn sàng đồng thuận (`eth_consensusReady`).
3. Xác minh tính toàn vẹn dữ liệu **Xapian DB**: Dữ liệu chỉ mục đã ghi trước khi tắt node không bị mất mát hay hư hại sau khi bật lại.
4. Kiểm chứng **Full Cluster Restart**: Tắt đồng loạt toàn bộ cụm node và bật lại, đảm bảo hệ thống tự khôi phục, tiếp tục xử lý giao dịch và 100% Zero-Fork (Block Hash & StateRoot trùng khớp tuyệt đối trên mọi node).

---

## 🔄 Luồng hoạt động chi tiết từng bước (Phương án B: Deploy & Tích lũy liên tục)

```
[BẮT ĐẦU VÒNG N]
   │
   ├─► [BƯỚC 1] Khởi động vòng & Deploy Contract mở đầu:
   │    ├── Bơm 15 TX warm-up toàn mạng.
   │    ├── 🚀 DEPLOY CONTRACT MỞ ĐẦU (Chạy full 6 bước Xapian).
   │    └── 💾 Lưu contract vào .xapian_recovery_contract.json (Danh sách tích lũy).
   │
   ├─► [BƯỚC 2] Rolling Restart lần lượt từng Node (Luân phiên Node 0 ➔ N):
   │    │
   │    ├─► Chặng con (Ví dụ: Chặng 2.1 Node 0, Chặng 2.2 Node 1...):
   │    │    ├── 1. Tắt Node N (Ansible stop --only-node N)
   │    │    ├── 2. Bơm 15 TX qua các node còn sống (nếu cluster > 3 nodes)
   │    │    ├── 3. Bật lại Node N (Ansible restart --only-node N)
   │    │    ├── 4. Chờ Node N online & consensus ready (eth_consensusReady)
   │    │    ├── 5. 🔍 [VERIFY-NODE] Xác minh TOÀN BỘ danh sách contracts đã tích lũy trên Node N
   │    │    │    └── (Đọc doc 0 tất cả contracts cũ + test QuerySearch "iphone" trên contract mới nhất)
   │    │    ├── 6. ⚡ Bơm 15 TX catch-up toàn cụm kiểm tra Zero-Fork
   │    │    ├── 7. 🚀 [DEPLOY XAPIAN MỚI QUA NODE N] Deploy contract Xapian mới toanh qua chính Node N:
   │    │    │    ├── Thực thi full 6 bước (Deploy ➔ Setup ➔ ReadBack ➔ Update ➔ QuerySearch ➔ View)
   │    │    │    └── 💾 Nạp thêm contract vào danh sách (oldest genesis + 99 most recent, max 100)
   │    │    └── 8. ✍️ [WRITE-DOC] Gửi TX ghi cập nhật bổ sung qua chính Node N
   │    │
   │    ... (Lặp lại chặng con cho toàn bộ các node: Node 0, Node 1, Node 2, Node 3, Node 4)
   │
   ├─► [BƯỚC 3] Full Cluster Restart (Tắt & Bật toàn bộ cụm node cùng lúc):
   │    ├── 1. Tắt toàn bộ cụm node (Ansible stop all)
   │    ├── 2. Nghỉ 5s giải phóng socket / process lock
   │    ├── 3. Bật lại toàn bộ cụm (Ansible restart all)
   │    ├── 4. Chờ 100% các node online & consensus ready
   │    ├── 5. Bơm 15 TX load-balance kiểm chứng sống/chết
   │    └── 6. 🌐 [VERIFY-CLUSTER] Đối chiếu chéo TOÀN BỘ danh sách contracts trên 100% nodes (Zero-Fork)
   │
   └─► [BƯỚC 4] Kiểm tra sức khỏe toàn cụm & Xác nhận 100% Zero-Fork ➔ KẾT THÚC VÒNG N
```

---

### 📋 Mô tả chi tiết từng bước:

#### 🏁 Chuẩn bị & Kiểm tra đầu vòng:
- **Quét mạng**: Tự động gọi RPC `eth_blockNumber` từ `config.json` để phát hiện các node đang sống (`ACTIVE_NODES`).
- **Pre-check Zero-Fork**: Chờ toàn bộ node đạt cùng chiều cao block, gọi `eth_getBlockByNumber` so khớp `hash` và `stateRoot` của tất cả các node.

#### 📦 Bước 1: Khởi động ban đầu & Deploy Contract Xapian mở đầu
- Gửi đợt giao dịch warm-up toàn cụm để kiểm tra đường truyền RPC.
- Gọi `xapian_tool --mode=setup --round=<round> --force-deploy`:
  - Deploy một hợp đồng `TestDbXapianV2` mở đầu cho vòng test.
  - Thực thi trọn vẹn **chu trình kiểm thử 6 bước đầy đủ**:
    1. **Deploy Contract**: Gửi bytecode tạo hợp đồng mới trên blockchain.
    2. **runStep1_Setup**: Khởi tạo database Xapian `products` và index 3 documents mẫu (Iphone 13 Pro, Samsung Galaxy S22, Macbook Pro 14).
    3. **runStep2_ReadBack**: Đọc ngược lại doc 0 từ database qua Event `Read_Data`.
    4. **runStep3_UpdateDoc**: Cập nhật nội dung doc 0 thành `"Iphone 13 Pro UPDATED"`.
    5. **runStep5b_QuerySearch**: Bơm giao dịch tìm kiếm full-text với từ khóa `"iphone"`, xác thực Event `Search_Item` trả về đúng document đã cập nhật.
    6. **runStep5c_GetData_View**: Gọi `eth_call` xác thực struct `ProductData` hoàn chỉnh được lưu trong storage.
  - **Chính sách lưu trữ & đối chiếu tối đa 100 contracts**:
    - Địa chỉ hợp đồng được lưu vào file [.xapian_recovery_contract.json](file:///home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-chain/.xapian_recovery_contract.json).
    - Áp dụng thuật toán pruning tối ưu: Luôn giữ lại **contract cũ nhất (Round 1 / Genesis)** và **99 contracts mới nhất** gần đây (`oldest + 99 most recent`).

#### 🔄 Bước 2: Rolling Restart từng Node (Luân phiên & Deploy liên tục qua từng node)
Lặp qua từng node trong danh sách active (Node 0 $\rightarrow$ Node 1 $\rightarrow$ Node 2 $\rightarrow$ Node 3 $\rightarrow$ Node 4):
1. **Tắt node**: Gọi `ansible_deploy.sh --stop --only-node <N>`, ghi cờ bỏ qua giám sát Telegram.
2. **Gửi TX khi thiếu node**: Nếu cụm còn $\ge 3$ nodes (đủ $2f+1$ quorum), gửi giao dịch qua các node còn lại để kiểm tra mạng vẫn tiến triển bình thường.
3. **Bật lại node**: Gọi `ansible_deploy.sh --restart --only-node <N>`.
4. **Chờ sẵn sàng**: Đợi RPC online và đợi tầng đồng thuận Rust báo `eth_consensusReady: true`.
5. **Kiểm tra Xapian sau restart**: Gọi `xapian_tool --mode=verify-node --target-node=<N>`:
   - Duyệt và xác minh **toàn bộ danh sách contracts** đã tích lũy (tối đa 100 contracts): Dùng `eth_call` đọc trực tiếp doc 0 $\rightarrow$ Xác nhận node vừa bật lại đọc đầy đủ toàn bộ dữ liệu của tất cả các contract trước đó mà không bị mất mát hay hư hỏng.
   - Với contract mới nhất: Gửi transaction tìm kiếm toàn văn `QuerySearch("iphone")` qua node vừa thức dậy.
6. **Bơm TX catch-up**: Gửi giao dịch qua toàn cụm để node vừa thức dậy đồng bộ đuổi kịp chiều cao.
7. 🚀 **Deploy Contract Xapian mới trực tiếp qua Node N** (Phương án B):
   - Gọi `xapian_tool --mode=setup --target-node=<N> --force-deploy` để deploy một contract mới toanh ngay qua cổng RPC của chính node vừa thức dậy.
   - Chạy đủ 6 bước khởi tạo và nạp vào danh sách lịch sử.
   - Giúp node kế tiếp khi thức dậy sẽ phải verify cả contract mới này!
8. **Bơm TX ghi Xapian**: Gọi `xapian_tool --mode=write-doc` qua Node N để chứng minh quyền ghi Xapian vẫn hoạt động tốt trên contract vừa deploy.

#### 🛑 Bước 3: Full Cluster Restart (Tắt & Bật toàn bộ cụm)
1. **Tắt toàn cụm**: Dừng tất cả các node cùng lúc (`ansible_deploy.sh --stop`).
2. **Nghỉ 5s**: Đảm bảo các tiến trình giải phóng port và disk lock.
3. **Bật lại toàn cụm**: Khởi động lại toàn bộ node (`ansible_deploy.sh --restart`).
4. **Chờ hội tụ**: Chờ 100% các node online và toàn bộ Validator đạt `eth_consensusReady`.
5. **Bơm TX kiểm chứng**: Gửi giao dịch load-balance qua toàn bộ cụm node.
6. **Đối chiếu Xapian toàn cụm**: Gọi `xapian_tool --mode=verify-cluster` đối chiếu dữ liệu của **toàn bộ danh sách contracts** (tối đa 100 contracts) trên từng node, đối chiếu chéo đảm bảo dữ liệu Xapian đồng nhất hoàn hảo trên toàn mạng (Zero-Fork).

#### 📡 Bước 4: Kiểm tra sức khỏe cuối cùng
- Quét qua từng node xác nhận không có node nào bị crash, treo hoặc rớt lại phía sau.
- Kết luận vòng test thành công, nghỉ 10s trước khi sang vòng tiếp theo (nếu chạy multi-loop).

---

## 💻 Cách thực thi

```bash
# Chạy mặc định 1 vòng lặp
./run_restart_test.sh

# Chạy 5 vòng lặp, mỗi chặng gửi 20 giao dịch
./run_restart_test.sh --loop 5 --count 20

# Chạy lặp vô tận (test qua đêm)
./run_restart_test.sh --infinite

# Chạy test liên tục trong 4 giờ
./run_restart_test.sh --duration-hours 4
```
