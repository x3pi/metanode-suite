# Kế hoạch Bổ sung Đa ngôn ngữ cho Xapian Stemmer (Phase 4)

Kế hoạch này giúp loại bỏ điểm hạn chế "Hardcode tiếng Anh" của thư viện Xapian trong hệ thống. Bằng cách bổ sung thêm cấu hình tham số `language` từ Smart Contract truyền xuống C++, người dùng DApp có thể tự quyết định việc sử dụng Stemmer (Rút gọn từ) cho tiếng Anh, tiếng Pháp... hoặc tắt hoàn toàn nó cho các ngôn ngữ đơn lập (Tiếng Việt, Tiếng Trung) nhằm tiết kiệm CPU và tăng độ chính xác của tìm kiếm.

## User Review Required

> [!WARNING]
> Đây là thay đổi có tác động tới ABI (Application Binary Interface). Phía Solidity (Smart Contract) cũng BẮT BUỘC phải thay đổi chữ ký hàm (Function Signature) tương ứng để khớp với bộ Decode dưới C++. Nếu C++ nâng cấp trước mà Solidity không đổi, lệnh gọi Xapian sẽ bị revert do lỗi Parse ABI.

## Proposed Changes

---

### [MODIFY] Phần Solidity (Smart Contract)
Tùy thuộc vào thiết kế thư viện Solidity hiện tại của bạn (`XapianDB.sol` hoặc tương đương), hãy cập nhật lại chữ ký hàm:
- **Lệnh Index Text:** Bổ sung `string memory language` (hoặc chèn vào struct `options` nếu có).
- **Lệnh Query Search:** Chèn thêm field `string language;` vào struct `SearchOptions`.

*(Agent đảm nhận phần này hãy phối hợp với file Solidity tương ứng để chèn biến)*.

---

### [MODIFY] [xapian_handlers.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/my_extension/xapian_handlers.cpp)

**1. Opcode: `XAPIAN_INDEX_TEXT_DOCUMENT` & `XAPIAN_V1_INDEX_TEXT_DOCUMENT`**
- Sửa chuỗi `inputABI` để thêm field `{"internalType": "string", "name": "language", "type": "string"}`.
- Lấy giá trị biến: `std::string language = input_argument.value("language", "none");` (Dùng "none" làm mặc định nếu không truyền).
- Cập nhật hàm gọi FFI: Truyền `language` vào hàm `manager->index_text(...)`.

**2. Opcode: `XAPIAN_QUERY_SEARCH` & `XAPIAN_V1_QUERY_SEARCH`**
- Sửa chuỗi `abi_string` để thêm `"language": "string"` vào bên trong mảng `components` của tuple `options`.
- Lấy giá trị biến: `std::string language = decodedData["options"].value("language", "none");`
- Cập nhật hàm gọi C++: Truyền `language` vào tham số cấu hình của `XapianSearcher` (ví dụ sửa hàm `searcher.search(...)` để nhận language).

---

### [MODIFY] [xapian_manager.h](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/include/xapian/xapian_manager.h)
- Sửa signature của `index_text`: Thêm `const std::string& language` vào danh sách tham số.
- Sửa định nghĩa struct `IndexTextData` trong `XapianLog`: Thêm trường `std::string language;` để lưu trữ nó vào Buffer `tx_buffers`.

---

### [MODIFY] [xapian_manager.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/xapian/xapian_manager.cpp)
**1. Hàm `index_text` và Buffer Logging:**
Lưu giá trị `language` vào biến cấu trúc `IndexTextData` trước khi đẩy vào RAM buffer.

**2. Hàm `replay_log` (Đoạn ghi xuống đĩa):**
Cập nhật block xử lý `INDEX_TEXT`:
```cpp
// Thay vì hardcode "english" như cũ:
// termgenerator.set_stemmer(Xapian::Stem("english"));

std::string lang = data.language;
if (lang == "none" || lang.empty()) {
    // Không dùng Stemmer (Tối ưu cho Tiếng Việt)
    // Bỏ qua lệnh set_stemmer hoặc gán "none" tùy version Xapian
} else {
    try {
        termgenerator.set_stemmer(Xapian::Stem(lang));
    } catch (...) {
        // Nút chặn an toàn: Nếu truyền sai tên ngôn ngữ, tắt stemmer thay vì crash
    }
}
```

---

### [MODIFY] [xapian_search.cpp](file:///home/abc/nhat/con-chain-v2/metanode/execution/pkg/mvm/linker/src/xapian/xapian_search.cpp)
- Cập nhật class/hàm chịu trách nhiệm biên dịch chuỗi query văn bản (`QueryParser`).
- Khởi tạo Stemmer tương ứng cho bộ Parser giống hệt như logic đã cài ở `replay_log`:
```cpp
if (lang != "none" && !lang.empty()) {
    try {
        queryparser.set_stemmer(Xapian::Stem(lang));
        queryparser.set_stemming_strategy(Xapian::QueryParser::STEM_SOME);
    } catch (...) {}
}
```

## Verification Plan

### Automated Tests
- Chạy script kiểm tra build để phát hiện các lỗi sai signature hàm:
```bash
cd ./consensus/metanode/scripts
./build_check.sh
```

### Manual Verification
- **Test 1 (Tiếng Việt - Tắt Stemmer):** Gửi `language = "none"`. Truyền chữ "những con mèo". Search chữ "mèo" -> Phải tìm ra.
- **Test 2 (Tiếng Anh - Bật Stemmer):** Gửi `language = "english"`. Truyền chữ "technologies". Search chữ "technology" -> Phải tìm ra.
- **Test 3 (Lỗi - An toàn):** Gửi `language = "tiengviet"`. Hệ thống không được crash mà tự động tụt về Fallback (không lỗi).
