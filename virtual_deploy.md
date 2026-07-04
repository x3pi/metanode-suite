# Kế hoạch Tối ưu Virtual Dependency (Phase 3)

Kế hoạch này nhắm đến việc giải quyết nút thắt cổ chai về hiệu suất (TPS bottleneck) gây ra bởi các khóa cấp độ cơ sở dữ liệu (Table-Level Locks) không cần thiết trong kiến trúc in-memory STM của Xapian.

## Mục tiêu
Loại bỏ hoàn toàn các lời gọi `injectVirtualDependency` sử dụng tham số `docId` rỗng (`""`). Việc này giúp:
1. **Mở khóa Write TPS:** Cho phép hàng nghìn giao dịch gọi `NEW_DOCUMENT` hoặc `DELETE_DOCUMENT` vào cùng một Database có thể chạy hoàn toàn song song (trước đây bị ép chạy tuần tự do xung đột Write-After-Write ở cấp độ DB).
2. **Mở khóa Read TPS:** Cho phép lệnh `QUERY_SEARCH` chạy song song với mọi giao dịch khác, tuân thủ mô hình Read-Committed Isolation (đọc dữ liệu của Block trước đó).

## User Review Required

> [!CAUTION]
> Đây là thay đổi can thiệp trực tiếp vào logic bắt xung đột của Block-STM. Sau khi loại bỏ khóa tổng cấp độ DB, các lệnh Search sẽ không còn chờ các lệnh thêm/xóa chạy xong trong cùng 1 Block nữa, mà nó sẽ trả về kết quả ngay lập tức (dựa trên trạng thái đĩa cứng của block cũ). Vui lòng xác nhận kiến trúc DApp của bạn chấp nhận hành vi Read-Committed này trước khi duyệt.

## Proposed Changes

---

### [MODIFY] [xapian_handlers.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/my_extension/xapian_handlers.cpp)

**1. Xóa khóa chặn của `XAPIAN_NEW_DOCUMENT` và `XAPIAN_V1_NEW_DOCUMENT`:**
Xóa dòng lệnh tiêm khóa DB (vì hàm `manager->new_document` sử dụng biến atomic thread-safe độc lập nên không cần lock toàn DB):
- `mvm::injectVirtualDependency(gs, address, input_argument["dbname"], "", false, true, this->txHash);`

**2. Xóa khóa chặn của `XAPIAN_DELETE_DOCUMENT` và `XAPIAN_V1_DELETE_DOCUMENT`:**
Xóa dòng lệnh tiêm khóa DB (chỉ giữ lại khóa Row-Level theo từng `docId` cụ thể):
- `mvm::injectVirtualDependency(gs, address, input_argument["dbname"], "", false, true, this->txHash);`

**3. Xóa khóa chặn của `XAPIAN_QUERY_SEARCH` và `XAPIAN_V1_QUERY_SEARCH`:**
Loại bỏ hoàn toàn logic kiểm tra `writerHash` thừa thãi (vì XapianSearcher vốn đọc trực tiếp từ physical DB chứ không qua overlay buffer):
```cpp
// XÓA HOÀN TOÀN CÁC DÒNG NÀY:
uint256_t writerHashValue = mvm::injectVirtualDependency(gs, address, dbName, "", true, false, nullptr);
uint256_t* writerHash = nullptr;
if (writerHashValue != 0 && writerHashValue != 1) {
    writerHash = &writerHashValue;
}
```

## Verification Plan

### Automated Tests
- Chạy script kiểm tra build để đảm bảo file `xapian_handlers.cpp` không lỗi syntax:
```bash
cd ./consensus/metanode/scripts
./build_check.sh
```

### Manual Verification
- **Test TPS (Benchmarking):** Bắn 1000 giao dịch `NEW_DOCUMENT` vào cùng 1 DB trong 1 block. So sánh chỉ số TPS và thời gian block processing (chắc chắn sẽ giảm từ vài giây xuống còn vài chục mili-giây do không bị re-execution).
- **Test Logic:** Bắn song song giao dịch `QUERY_SEARCH` và `NEW_DOCUMENT`, đảm bảo hệ thống không còn quăng log báo Conflict (Xung đột).
