# 38-xapian-multinode-sync

## Mục tiêu kiểm thử
Kiểm chứng E2E tính nhất quán dữ liệu tuyệt đối giữa các Node trong cụm đồng thuận Raft (`m0`, `m1`, `m2` trong `rpc_nodes`) và các `sync_nodes` (nếu có) đối với cơ sở dữ liệu toàn văn Xapian (`FullDatabaseV1` tại địa chỉ `0x107`).

## Kịch bản chi tiết
1. **Bước 1 (Deploy & Baseline):** Deploy hợp đồng `XapianMultiNodeSync` mới (mỗi lần chạy dùng địa chỉ hợp đồng độc lập để cô lập dữ liệu Xapian) và tạo 3 tài liệu gốc (`baseline[0..2]` chiếm physical docid 1, 2, 3) cùng 1 tài liệu bộ đếm (`counterDocId`).
2. **Bước 2 (Bảo vệ Virtual DocID ngắn qua Follower Forwarding):** Gửi giao dịch qua node thứ hai (`m1`) ghi tài liệu có định danh số nhỏ (`docId = 2`) để xác nhận không ghi đè nhầm vào `baseline[1]` đang nằm tại physical docid 2 trên cả Leader lẫn Follower.
3. **Bước 3 (Cấp phát Physical DocID tất định dưới Block-STM song song):** Gửi đồng thời N giao dịch từ nhiều ví rải đều qua tất cả RPC node (`m0`, `m1`, `m2`), cùng index chuỗi từ khóa giống hệt nhau (`"metanode equalrank consensus"`). Vì điểm liên quan BM25 bằng nhau tuyệt đối, Xapian phân định thứ tự dựa trên physical docid — kiểm chứng Leader (chạy song song Block-STM) và Follower/SyncNode (replay `FullDbLogs`) cấp phát physical docid trùng khớp 100%.
4. **Bước 4 (Đụng độ ghi chung Xapian + EVM dưới Block-STM):** Gửi đồng thời N giao dịch gọi `incrementSharedCounter()` tăng cả biến EVM lẫn dữ liệu Xapian.
5. **Bước 5 (Cập nhật, xóa tài liệu & bảo vệ xóa ID ngắn):** Cập nhật `baseline[0]`, xóa `baseline[2]`, và gọi `deleteDocument(1)` (đảm bảo không xóa nhầm physical docid 1).
6. **Bước 6 (Đối chiếu Byte-for-Byte trên mọi Node):** Đợi toàn bộ node đồng bộ tới block cuối cùng, sau đó gọi `eth_call` trực tiếp vào từng node (`m0`, `m1`, `m2`, `sync_nodes`) để so khớp từng byte kết quả `getTrackedIds`, `readSharedCounter`, `readDocSnapshot` và 3 truy vấn `queryCluster` (BM25 tie-break, sắp xếp tăng dần theo Value Slot 0, và xếp hạng BM25 sau khi boost trọng số).

## Cách chạy
- Chạy qua bộ test tổng hợp cho `chain_a`:
  `./run_all_tests.sh --chain=chain_a`
- Hoặc chạy riêng lẻ:
  `TARGET_CHAIN=chain_a go run .`
  `TARGET_CHAIN=chain_a go test -v -count=1 .`

