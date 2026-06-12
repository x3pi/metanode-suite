# Download File Flow (Request/Response)

Tài liệu này mô tả chi tiết quy trình (flow) cùng với cấu trúc Request và Response khi thực hiện tải xuống một file thông qua giao thức WebTransport trên dApp.

## 1. Tương tác với Smart Contract (Blockchain)

Trước khi tải file từ WebTransport Server, Client phải làm việc với Blockchain để lấy thông tin và cấp quyền tải xuống.

1.  **Lấy thông tin file:**
    *   **Call:** `getFileInfo(fileKey)`
    *   **Response:** File info (tổng số `totalChunks`, `name`...). Nếu `totalChunks == 0`, file không tồn tại.
2.  **Tính phí:**
    *   **Call:** `calculatePrice(totalChunks)`
    *   **Response:** Phí cần thanh toán (BigInt).
3.  **Thanh toán lấy DownloadKey:**
    *   **Transaction:** `payForDownload(fileKey, 1)` với `value` bằng phí ở trên.
    *   **Event Log:** Chờ transaction thành công, bắt sự kiện `DownloadKeyGenerated` để lấy mã `downloadKey`.

---

## 2. Tạo Chữ Ký Xác Thực (Signature)

WebTransport Server yêu cầu xác thực bảo mật trước khi trả chunk. Client phải tạo chữ ký:

1.  **Format chuỗi gốc:** `"0x00" + download_key_bỏ_0x`
2.  **Băm chuỗi:** `messageHash = keccak256(stringToHex(chuỗi_trên))`
3.  **Ký hash:** Dùng private key của ví để ký (không kèm prefix "Ethereum Signed Message").
4.  **Tạo Hex Signature:** Kết hợp `r`, `s` và `v` thành chuỗi Hex dài 65 bytes.

---

## 3. Giao tiếp WebTransport (QUIC)

Client sẽ kết nối song song tới các server (ví dụ `DOWNLOAD_SERVER_1`, `DOWNLOAD_SERVER_2`) và tải các chunks theo dạng **Bidirectional Stream**.

### 3.1. Request Frame (Client ➡️ Server)

Mỗi request tải chunk được mã hóa thành một khung nhị phân (Binary Frame) có định dạng:
`[4 bytes BE uint32: Payload Length] [JSON bytes (UTF-8)]`

**Cấu trúc JSON (Request):**
```json
{
  "id": "uuid_của_request",
  "command": "download_chunk",
  "payload": {
    "download_key": "Mã download key (không có 0x)",
    "chunk_index": 0,    // Số thứ tự chunk cần tải
    "signature": "Chữ ký 65 bytes (Hex string)"
  }
}
```

### 3.2. Response Frame (Server ➡️ Client)

Khi server xử lý xong, nó trả về một Binary Frame chứa cả JSON Header và Dữ liệu thô (Raw Data).
Định dạng Frame trả về:
`[4 bytes BE uint32: Payload Length] [2 bytes BE uint16: JSON Length] [JSON header bytes] [Raw Chunk Bytes (Dữ liệu file)]`

**Cấu trúc JSON Header (Thành công):**
```json
{
  "id": "uuid_của_request",
  "command": "download_chunk"
}
```
👉 *Dữ liệu liền kề sau JSON Header chính là nội dung nhị phân (ArrayBuffer) của chunk đó.*

**Cấu trúc JSON Header (Lỗi):**
```json
{
  "status": "error",
  "message": "Nội dung lỗi chi tiết từ server"
}
```

---

## 4. Ráp file và Hoàn tất

*   Client chia các request tải chunk thành từng mẻ (batch) để chạy song song (Concurrency limit).
*   Nếu lỗi sẽ tự động thử lại (Retry max 3 lần).
*   Sau khi thu thập đủ mảng `ArrayBuffer` của tất cả chunks, Client ghép nối chúng đúng theo `chunkIndex`.
*   Tạo `Blob` đối tượng từ mảng đã ghép và kích hoạt URL tạo sẵn để trình duyệt tải file xuống.
