# Hướng Dẫn Sử Dụng `compile.js` (Solidity Compiler)

Script Node.js dùng để biên dịch hợp đồng Solidity (`.sol`) ra **ABI** và **Bytecode (bin)** tương thích với Metanode EVM.

---

## 1. Cú pháp cơ bản
```bash
node compile.js <đường_dẫn_file_sol> <thư_mục_output>
```

**Ví dụ:**
```bash
node compile.js test-counter.sol ./build
```

---

## 2. Cách chạy nhanh (sử dụng node_modules bên dự án kế bên)
Nếu chưa cài đặt `solc` ở thư mục hiện tại, dùng lệnh sau:

```bash
NODE_PATH=/home/abc/nhat/consensus-chain/mtn-simple-2025/cmd/tool/tool-test-chain/node_modules \
node compile.js test-counter.sol ./build
```

---

## 3. Lưu ý quan trọng
*   **Không tạo lỗi `PUSH0`**: Script tự động cấu hình `evmVersion: "london"`. Metanode EVM không hỗ trợ opcode `PUSH0` (có từ bản Shanghai trở đi).
*   **Tên file đầu ra**: Sẽ có định dạng `{Tên_File_Sol}_{Tên_Contract}.abi` và `{Tên_File_Sol}_{Tên_Contract}.bin` nằm trong thư mục output đã chọn.
