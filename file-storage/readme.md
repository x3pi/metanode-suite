## Upload File (TCP Mode)

Ở chế độ TCP, lưu ý khi upload file:
- Kích thước khi upload 1 chunk là **1MB**.
- Nếu nhận được command báo thành công như sau:
  ```go
  if cmd == command.TransactionSuccess {
  	return transaction.Hash(), nil
  }
  ```
  Điều này có nghĩa là quá trình upload file đã thành công.


## Nhận ChunkData ở RawQuic

Đoạn code sau mô tả những thay đổi về việc nhận chunk data trong `rawquic`:

``` go
func RequestChunkFromRustServerQuic(
	conn quic.Connection,
	fileKey string,
	downloadKey string,
	chunkIndex int, sign string) ([]byte, error) {
	// Mở stream
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("❌ [Chunk %d] Mở stream FAILED: %v", chunkIndex, err)
		return nil, fmt.Errorf("không thể mở stream: %v", err)
	}
	defer stream.Close()

	// Tạo request
	request := models.DownloadChunkRequest{
		Command: "DownloadChunkRequest",
		Payload: models.DownloadChunkPayload{
			FileKey:     fileKey,
			DownloadKey: downloadKey,
			ChunkIndex:  chunkIndex,
			Signature:   sign,
		},
	}
	// Encode JSON và gửi với length prefix
	jsonData, err := json.Marshal(request)
	if err != nil {
		log.Printf("❌ [Chunk %d] JSON Marshal FAILED: %v", chunkIndex, err)
		return nil, fmt.Errorf("lỗi khi encode request: %v", err)
	}
	// Thêm \n để Rust server parse JSON
	jsonData = append(jsonData, '\n')

	if err := writeFrameWithLength(stream, jsonData); err != nil {
		log.Printf("❌ [Chunk %d] Gửi frame FAILED: %v", chunkIndex, err)
		return nil, fmt.Errorf("lỗi khi gửi request: %v", err)
	}

	responseData, err := readFrameWithLength(stream)
	if err != nil {
		log.Printf("❌ [Chunk %d] Đọc frame FAILED: %v", chunkIndex, err)
		return nil, fmt.Errorf("lỗi khi đọc response: %v", err)
	}

	var response models.DownloadResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		log.Printf("❌ [Chunk %d] Parse JSON FAILED: %v", chunkIndex, err)
		log.Printf("📄 [Chunk %d] Raw response (first 200 bytes): %s",
			chunkIndex, string(responseData[:min(len(responseData), 200)]))
		return nil, fmt.Errorf("lỗi khi parse response: %v", err)
	}

	if response.Status != "SUCCESS" {
		log.Printf("❌ [Chunk %d] Server báo lỗi: %v", chunkIndex, response.Message)
		return nil, fmt.Errorf("server báo lỗi: %v", response.Message)
	}

	// Đọc Frame 2: Dữ liệu nhị phân thô
	chunkData, err := readFrameWithLength(stream)
	if err != nil {
		log.Printf("❌ [Chunk %d] Đọc frame 2 FAILED: %v", chunkIndex, err)
		return nil, fmt.Errorf("lỗi khi đọc chunk data: %v", err)
	}
	return chunkData, nil
}
```

## ABI mới cho Remove Whitelist

```json
{
        "inputs": [
            {
                "internalType": "bytes32",
                "name": "fileKey",
                "type": "bytes32"
            },
            {
                "internalType": "address[]",
                "name": "users",
                "type": "address[]"
            }
        ],
        "name": "removeWhitelist",
        "outputs": [],
        "stateMutability": "nonpayable",
        "type": "function"
    },
```