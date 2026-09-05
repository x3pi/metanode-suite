# 🚀 Hướng Dẫn Kỹ Thuật: Cơ Chế Tải File (Upload) Trực Tiếp Qua Rust Node (Raw QUIC & WebTransport)

Tài liệu này hướng dẫn chi tiết về kiến trúc và cách triển khai tải file thế hệ mới trong hệ thống Metanode File Storage: **kết hợp giữa Smart Contract (quản lý metadata & thanh toán) và Rust Storage Nodes (lưu trữ phân tán tốc độ cao qua QUIC / WebTransport)**.

---

## 📌 1. Tổng Quan: Nguyên Lý Upload Tương Tự Như Download (Hybrid Architecture)

Cơ chế **Upload mới hoạt động theo nguyên lý hoàn toàn đối xứng và tương tự như Download**:

### 1.1. Tách biệt Control Plane (Blockchain) và Data Plane (Rust Storage)
- **Trước đây (RPC thuần Smart Contract)**:
  Toàn bộ dữ liệu nhị phân (mỗi chunk 1MB) phải đi qua RPC của Blockchain thông qua giao dịch EVM. Cách này cực kỳ tốn gas, làm phình to blockchain và bị nghẽn tốc độ nghiêm trọng.
- **Hiện nay (Mô hình Hybrid tương tự Download)**:
  - **Control Plane (Blockchain)**: Chỉ gọi smart contract 1 lần duy nhất ở đầu quy trình (`pushFileInfo` cho Upload tương tự như kiểm tra quyền / mua slot ở Download) để lưu Merkle Root, tính phí và phát hành `fileKey`.
  - **Data Plane (Rust Nodes qua QUIC)**: Toàn bộ quá trình truyền tải dữ liệu nhị phân (chunks) đều đi **trực tiếp qua các Rust Storage Nodes** bằng giao thức **QUIC** (tốc độ mạng thực tế, hàng chục đến hàng trăm MB/s, không tốn gas).

### 1.2. Tính đối xứng giữa Download và Upload

| Đặc tính | 📥 Download (Tải xuống) | 📤 Upload (Tải lên) |
| :--- | :--- | :--- |
| **Xác thực quyền** | Ký offline `downloadKey` xác thực quyền tải. | Ký offline `keccak256("0x00" + fileKey + merkleRoot)` chứng minh quyền Owner. |
| **Kênh truyền tải** | Kết nối trực tiếp tới Rust node qua **QUIC / WebTransport**. | Kết nối trực tiếp tới Rust node qua **QUIC / WebTransport**. |
| **Giao thức Wire Frame** | Dùng chung chuẩn frame (4-byte length prefix hoặc WebTransport payload frame). | Dùng chung chuẩn frame (4-byte length prefix hoặc WebTransport payload frame). |
| **Luồng Stream** | Client gửi JSON Request $\rightarrow$ Server trả về nhị phân chunk. | Client gửi JSON Request + chunk nhị phân $\rightarrow$ Server trả về `{ "status": "SUCCESS" }`. |
| **Cân bằng tải** | Tải luân phiên (round-robin) chunks từ nhiều Rust node. | Đẩy luân phiên (round-robin) chunks tới nhiều Rust node. |

---

## 🔌 2. Triển Khai Raw QUIC (Golang / Desktop / Backend)

Thích hợp cho Go, ứng dụng Desktop (C++, Rust), Mobile native hoặc các worker dịch vụ backend.

### 2.1. Đặc Tả Giao Thức (Protocol Specification)
- **Transport**: QUIC (thư viện Go: `github.com/quic-go/quic-go`).
- **ALPN**: `file-storage-v1`.
- **TLS**: Chấp nhận self-signed certificates (`InsecureSkipVerify: true`).
- **Frame Codec**: Giao tiếp theo chuẩn `tokio_util::codec::LengthDelimitedCodec`:
  Mỗi frame gồm **4-byte Big-Endian uint32** chỉ độ dài, theo sau là dữ liệu payload.

```
┌─────────────────────────┬──────────────────────────────────────────┐
│ Length (4 bytes, BE)    │ Payload Data                             │
└─────────────────────────┴──────────────────────────────────────────┘
```

### 2.2. Trình Tự Gửi 1 Chunk
Mỗi chunk được tải lên trên một bidirectional stream riêng biệt (`conn.OpenStreamSync`):
1. **Frame 1 (JSON Command Header)**:
   - Dữ liệu JSON kèm ký tự xuống dòng `\n`:
     ```json
     {
       "command": "UploadChunk",
       "payload": {
         "contract_address": "0x...",
         "file_key": "64_hex_chars",
         "chunk_index": 0,
         "signature": "130_hex_chars",
         "merkle_proof_hashes": ["hex_hash_1", "hex_hash_2", ...],
         "merkle_root": "64_hex_chars"
       }
     }\n
     ```
2. **Frame 2 (Raw Binary Chunk)**:
   - Toàn bộ nội dung nhị phân thô của chunk (tối đa 1MB).
3. **Đọc Response Frame**:
   - Đọc frame độ dài từ server, parse JSON:
     ```json
     { "status": "SUCCESS", "message": "Chunk uploaded successfully" }
     ```

### 2.3. Code Mẫu Golang

```go
package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/quic-go/quic-go"
)

// Helper: Ghi frame độ dài 4-byte Big Endian
func writeFrameWithLength(stream quic.Stream, data []byte) error {
	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(data)))
	if _, err := stream.Write(lengthBuf); err != nil {
		return err
	}
	_, err := stream.Write(data)
	return err
}

// Helper: Đọc frame độ dài 4-byte Big Endian
func readFrameWithLength(stream quic.Stream) ([]byte, error) {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(stream, lengthBuf); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)
	if length > 8*1024*1024 {
		return nil, fmt.Errorf("frame vượt quá giới hạn 8MB: %d", length)
	}
	data := make([]byte, length)
	_, err := io.ReadFull(stream, data)
	return data, err
}

// Khởi tạo kết nối QUIC tới Rust Server
func ConnectRustQuic(addr string) (quic.Connection, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"file-storage-v1"},
	}
	quicConf := &quic.Config{
		MaxIdleTimeout:       120 * time.Second,
		HandshakeIdleTimeout: 30 * time.Second,
		KeepAlivePeriod:      15 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return quic.DialAddr(ctx, addr, tlsConf, quicConf)
}

// Gửi 1 chunk tới Rust Server
func UploadChunkRawQuic(
	conn quic.Connection,
	contractAddr, fileKeyHex, merkleRootHex, sigHex string,
	chunkIndex int,
	chunkData []byte,
	proofHex []string,
) error {
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return fmt.Errorf("không thể mở stream: %w", err)
	}
	defer stream.Close()

	// 1. Gửi Frame 1: JSON Command
	payload := map[string]interface{}{
		"command": "UploadChunk",
		"payload": map[string]interface{}{
			"contract_address":    contractAddr,
			"file_key":            fileKeyHex,
			"chunk_index":         chunkIndex,
			"signature":           sigHex,
			"merkle_proof_hashes": proofHex,
			"merkle_root":         merkleRootHex,
		},
	}
	jsonData, _ := json.Marshal(payload)
	jsonData = append(jsonData, '\n') // Rust server yêu cầu newline

	if err := writeFrameWithLength(stream, jsonData); err != nil {
		return fmt.Errorf("lỗi gửi metadata frame: %w", err)
	}

	// 2. Gửi Frame 2: Raw Binary Data
	if err := writeFrameWithLength(stream, chunkData); err != nil {
		return fmt.Errorf("lỗi gửi binary chunk frame: %w", err)
	}

	// 3. Đọc Response Frame
	respBytes, err := readFrameWithLength(stream)
	if err != nil {
		return fmt.Errorf("lỗi đọc response: %w", err)
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("parse response json thất bại: %w", err)
	}
	if resp.Status != "SUCCESS" && resp.Status != "COMPLETED" {
		return fmt.Errorf("server từ chối chunk: %s", resp.Message)
	}

	return nil
}
```

---

## 🌐 3. Triển Khai WebTransport (Trình Duyệt / Web3 DApp Frontend)

Do trình duyệt web chặn kết nối raw UDP/QUIC socket tùy ý, các ứng dụng Web3 bắt buộc phải dùng chuẩn W3C **WebTransport (HTTP/3)** kết nối đến endpoint: `https://<ip>:<port>/quic`.

### 3.1. Đặc Tả Frame Format của WebTransport (Binary Multiplexing)

Để đạt tốc độ tối đa và tránh cấp phát/copy dữ liệu lớn trong RAM trình duyệt, WebTransport đóng gói theo cấu trúc:

```
┌────────────────────────┬──────────────────────┬─────────────────┬──────────────────────┐
│ Total Payload Length   │ JSON Header Length   │ JSON Header     │ Raw Chunk Binary     │
│ (4 bytes, Big-Endian)  │ (2 bytes, BigEndian) │ (UTF-8 bytes)   │ (Optional, n bytes)  │
└────────────────────────┴──────────────────────┴─────────────────┴──────────────────────┘
```

- **Total Payload Length** = $2 + \text{len(JSON)} + \text{len(RawChunk)}$
- **JSON Header Length** = $\text{len(JSON)}$

### 3.2. Kỹ Thuật Tối Ưu Zero-Copy Trên Trình Duyệt
1. **Header-Only Encoding**: Chỉ encode phần 4 byte + 2 byte + JSON string thành một buffer nhỏ (~250 bytes).
2. **Direct Stream Pipeline**: Ghi buffer Header vào stream, sau đó ghi trực tiếp `chunkData` (`Uint8Array` 1MB cắt từ `file.slice()`) vào `writer` mà không dùng `concat` hay copy thêm một mảng mới.
3. **Web Worker Merkle Hashing**: Băm Merkle Tree trong Web Worker riêng biệt (`merkle.worker.ts`) để tránh đơ giao diện React/Vue khi xử lý file từ vài trăm MB đến vài GB.

### 3.3. Code Mẫu TypeScript / WebTransport

#### `wtTransport.ts`
```typescript
export interface WtUploadRequest {
  id: string;
  command: "upload_chunk";
  payload: {
    contract_address: string;
    file_key: string;
    chunk_index: number;
    signature: string;
    merkle_proof_hashes: string[];
    merkle_root: string;
  };
}

// Zero-copy: Encode phần Header riêng biệt
export function encodeHeaderOnly(obj: object, dataLen: number): Uint8Array {
  const jsonStr = JSON.stringify(obj);
  const jsonBytes = new TextEncoder().encode(jsonStr);
  const payloadLen = 2 + jsonBytes.length + dataLen;

  const header = new Uint8Array(4 + 2 + jsonBytes.length);
  const view = new DataView(header.buffer);

  view.setUint32(0, payloadLen, false);       // 4 bytes: tổng payload
  view.setUint16(4, jsonBytes.length, false); // 2 bytes: độ dài json
  header.set(jsonBytes, 6);

  return header;
}

// Gửi chunk qua WebTransport Bidirectional Stream
export async function pushChunkOnStream(
  transport: WebTransport,
  contractAddress: string,
  fileKey: string,
  chunkIndex: number,
  chunkData: Uint8Array,
  signature: string,
  merkleProofHashes: string[],
  merkleRoot: string
): Promise<{ ok: boolean; error?: string }> {
  const stream = await transport.createBidirectionalStream();
  const writer = stream.writable.getWriter();
  const reader = stream.readable.getReader();

  try {
    const request: WtUploadRequest = {
      id: crypto.randomUUID(),
      command: "upload_chunk",
      payload: {
        contract_address: contractAddress,
        file_key: fileKey.replace(/^0x/, ""),
        chunk_index: chunkIndex,
        signature,
        merkle_proof_hashes: merkleProofHashes,
        merkle_root: merkleRoot,
      },
    };

    // 1. Ghi Header
    const header = encodeHeaderOnly(request, chunkData.length);
    await writer.write(header);

    // 2. Ghi trực tiếp Chunk Data (Zero copy)
    await writer.write(chunkData);
    await writer.close();
    writer.releaseLock();

    // 3. Đọc response
    const chunks: Uint8Array[] = [];
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (value) chunks.push(value);
    }

    // Ghép và parse response frame
    const totalLen = chunks.reduce((acc, c) => acc + c.length, 0);
    const bytes = new Uint8Array(totalLen);
    let offset = 0;
    for (const c of chunks) {
      bytes.set(c, offset);
      offset += c.length;
    }

    const jsonLen = new DataView(bytes.buffer).getUint16(4, false);
    const jsonBytes = bytes.subarray(6, 6 + jsonLen);
    const res = JSON.parse(new TextDecoder().decode(jsonBytes));

    if (res.status === "error") {
      return { ok: false, error: res.message };
    }
    return { ok: true };
  } finally {
    reader.releaseLock();
  }
}
```

#### Upload Pipeline Trong React / Web3 Client (`fileUpload.ts`)
```typescript
import { pushChunkOnStream } from "./wtTransport";

export async function uploadChunksInParallel(
  file: File,
  fileKey: string,
  merkleRoot: string,
  signatureHex: string,
  proofs: string[][],
  serverUrls: string[]
) {
  const CHUNK_SIZE = 1024 * 1024; // 1MB
  const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
  const CONCURRENCY = 10; // 10 workers song song

  // Khởi tạo kết nối WebTransport tới các Rust servers
  const transports = serverUrls.map(url => new WebTransport(`${url}/quic`));
  await Promise.all(transports.map(t => t.ready));

  try {
    let chunkCursor = 0;
    const worker = async () => {
      while (chunkCursor < totalChunks) {
        const index = chunkCursor++;
        const transport = transports[index % transports.length]; // Luân phiên servers

        const start = index * CHUNK_SIZE;
        const end = Math.min(start + CHUNK_SIZE, file.size);
        const chunkBlob = file.slice(start, end);
        const chunkData = new Uint8Array(await chunkBlob.arrayBuffer());

        const res = await pushChunkOnStream(
          transport,
          CONTRACT_ADDRESS,
          fileKey,
          index,
          chunkData,
          signatureHex,
          proofs[index],
          merkleRoot
        );

        if (!res.ok) {
          throw new Error(`Upload chunk ${index} thất bại: ${res.error}`);
        }
      }
    };

    // Chạy song song CONCURRENCY workers
    const workers = Array.from({ length: CONCURRENCY }, () => worker());
    await Promise.all(workers);
    console.log("✅ Tải toàn bộ file hoàn tất 100%!");
  } finally {
    transports.forEach(t => t.close());
  }
}
```