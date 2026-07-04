# Kế hoạch tối ưu hóa Đọc Song Song cho Xapian DB (Phase 1)

Kế hoạch này tập trung giải quyết nút thắt cổ chai (bottleneck) khiến các hàm đọc từ Xapian buffer hiện tại đang bị khóa tuần tự (chỉ chạy 1 luồng). Việc thay thế `std::mutex` bằng `std::shared_mutex` giúp hệ thống cho phép đọc đa luồng trên in-memory buffer mà vẫn đảm bảo an toàn. 

*(Vấn đề xóa bỏ mvmId sẽ được đưa vào một Plan riêng (Phase 2) sau khi hoàn thành Phase 1 này, để đảm bảo an toàn và dễ kiểm thử)*.

## User Review Required

> [!IMPORTANT]
> Đây là thay đổi về Mutex (Multi-threading). Mặc dù thay đổi nhỏ nhưng tôi cần bạn xác nhận để triển khai. Sự thay đổi này sẽ giúp MVM tăng tốc độ thực thi cho các giao dịch gọi read opcodes trên Xapian.

## Proposed Changes

---

### MVM Linker

#### [MODIFY] [xapian_manager.h](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/include/xapian/xapian_manager.h)
- Nâng cấp biến `std::mutex tx_buffers_mutex;` thành `mutable std::shared_mutex tx_buffers_mutex;`. Từ khóa `mutable` giúp khóa có thể hoạt động trong các hàm có tính chất read-only.

#### [MODIFY] [xapian_manager.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/xapian/xapian_manager.cpp)
Sửa lỗi dùng sai lock cho các hàm truy xuất:
- **Hàm Đọc (Read ops):** Tại các hàm `get_data`, `get_value`, `get_document`, `get_terms`, chuyển `std::unique_lock<std::shared_mutex> read_lock(changes_mutex);` thành `std::shared_lock<std::shared_mutex> read_lock(changes_mutex);` để cấp quyền đọc song song.
- **Hàm Đọc Buffer:** Tại `get_overlayed_document`, đổi `std::lock_guard<std::mutex> buffer_lock(tx_buffers_mutex);` thành `std::shared_lock<std::shared_mutex> buffer_lock(tx_buffers_mutex);`.
- **Hàm Ghi (Write ops):** Tại các hàm ghi như `new_document`, `delete_document`, `add_value`, `add_term`, `index_text`, `set_data`, cập nhật thành khóa ghi độc quyền: `std::unique_lock<std::shared_mutex> lock2(tx_buffers_mutex);`.

#### [MODIFY] [xapian_registry.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/xapian/xapian_registry.cpp)
Cập nhật theo loại biến mutex mới:
- Tại các hàm `clearBufferForTxHash` và `commitBufferForTxHash` (dùng để xóa hoặc commit buffer), cập nhật `std::lock_guard<std::mutex>` thành `std::unique_lock<std::shared_mutex> lock(manager_ptr->tx_buffers_mutex);`.

## Verification Plan

### Automated Tests
- Chạy script kiểm tra build để đảm bảo code C++ biên dịch thành công:
```bash
cd ./consensus/metanode/scripts
./build_check.sh
```

### Manual Verification
- Deploy node mạng và chạy các giao dịch Xapian (đặc biệt là các giao dịch query search đan xen với insert/update).
- Đảm bảo hiệu năng được cải thiện và không gặp hiện tượng treo luồng (deadlock).
