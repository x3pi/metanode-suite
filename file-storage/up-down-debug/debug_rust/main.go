package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// Danh sách tất cả các server cần lấy log
// var RUST_SERVERS = []string{
// 	"192.168.1.233:7081",
// 	"192.168.1.233:7082",
// }

var RUST_SERVERS = []string{
	"206.189.152.114:7081",
	// "157.245.202.80:7082",
}

// --- CÁC STRUCT CŨ (CHO ListChunks) ---

type ListChunksPayload struct {
	FileKey string `json:"file_key"`
}
type ListChunksRequest struct {
	Command string            `json:"command"`
	Payload ListChunksPayload `json:"payload"`
}
type ListChunksResponse struct {
	Status       string   `json:"status"`
	Message      string   `json:"message"`
	ChunkIndices []uint64 `json:"chunk_indices"`
}

// --- CÁC STRUCT MỚI (CHO API LOG ĐÃ TÁCH) ---

// --- API 1: GetLogList ---
type GetLogListRequest struct {
	Command string      `json:"command"`
	Payload interface{} `json:"payload,omitempty"` // Sẽ gửi nil
}

type LogsListResponse struct {
	Status         string   `json:"status"`
	Message        string   `json:"message"`
	AvailableFiles []string `json:"available_files"`
}

// --- API 2: GetLogContent ---
type GetLogContentPayload struct {
	FileName string `json:"file_name"`
}
type GetLogContentRequest struct {
	Command string               `json:"command"`
	Payload GetLogContentPayload `json:"payload"`
}

type LogFileContent struct {
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}
type LogsContentResponse struct {
	Status     string          `json:"status"`
	Message    string          `json:"message"`
	LogContent *LogFileContent `json:"log_content"`
}

// --- CÁC HÀM HELPER (Giữ nguyên) ---

// writeFrameWithLength gửi data với 4-byte big-endian length prefix
func writeFrameWithLength(stream quic.Stream, data []byte) error {
	length := uint32(len(data))
	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, length)
	if _, err := stream.Write(lengthBuf); err != nil {
		return fmt.Errorf("lỗi gửi length: %v", err)
	}
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("lỗi gửi data: %v", err)
	}
	return nil
}

// readFrameWithLength đọc data với 4-byte big-endian length prefix
func readFrameWithLength(stream quic.Stream) ([]byte, error) {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(stream, lengthBuf); err != nil {
		return nil, fmt.Errorf("lỗi đọc length: %v", err)
	}
	length := binary.BigEndian.Uint32(lengthBuf)
	if length > 8*1024*1024 { // 8MB limit
		return nil, fmt.Errorf("frame quá lớn: %d bytes", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(stream, data); err != nil {
		return nil, fmt.Errorf("lỗi đọc data: %v", err)
	}
	return data, nil
}

// CreateQuicConnection
func CreateQuicConnection(serverAddr string) (quic.Connection, error) {
	var tlsConf *tls.Config
	certPath := "./certificate.pem" // Thử load từ thư mục hiện tại
	// if _, err := os.Stat(certPath); err == nil {
	// 	// File certificate tồn tại, load và dùng để verify
	// 	log.Printf("🔐 Loading certificate from %s for TLS verification", certPath)
	// 	certPEM, err := os.ReadFile(certPath)
	// 	if err != nil {
	// 		log.Printf("⚠️ Failed to read certificate file: %v, falling back to InsecureSkipVerify", err)
	// 		tlsConf = &tls.Config{
	// 			InsecureSkipVerify: true,
	// 			NextProtos:         []string{"file-storage-v1"},
	// 		}
	// 	} else {
	// 		certPool := x509.NewCertPool()
	// 		if !certPool.AppendCertsFromPEM(certPEM) {
	// 			log.Printf("⚠️ Failed to parse certificate from %s, falling back to InsecureSkipVerify", certPath)
	// 			tlsConf = &tls.Config{
	// 				InsecureSkipVerify: true,
	// 				NextProtos:         []string{"file-storage-v1"},
	// 			}
	// 		} else {
	// 			log.Printf("✅ Certificate loaded successfully, enabling TLS verification (skipping hostname/IP verification)")
	// 			// Verify certificate signature nhưng skip hostname/IP verification
	// 			// Dùng ServerName = "localhost" vì certificate có "localhost" trong SANs
	// 			tlsConf = &tls.Config{
	// 				RootCAs:    certPool,
	// 				ServerName: "localhost", // Dùng hostname có trong certificate
	// 				NextProtos: []string{"file-storage-v1"},
	// 			}
	// 		}
	// 	}
	// } else {
	// Không có certificate file, bật verification nhưng sẽ fail nếu server dùng self-signed
	log.Printf("⚠️ Certificate file not found (%s), attempting TLS verification (may fail if server uses self-signed cert)", certPath)
	tlsConf = &tls.Config{
		InsecureSkipVerify: true, // ✅ BẬT TLS VERIFICATION (TEST)
		NextProtos:         []string{"file-storage-v1"},
	}
	// }
	var conn quic.Connection
	var err error
	const maxRetries = 3
	const retryDelay = 200 * time.Millisecond
	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err = quic.DialAddr(ctx, serverAddr, tlsConf, nil)
		cancel()
		if err == nil {
			log.Printf("✅ Kết nối QUIC thành công đến %s", serverAddr)
			return conn, nil
		}
		log.Printf("⚠️ Kết nối QUIC đến %s FAILED (Lần thử %d/%d): %v", serverAddr, i+1, maxRetries, err)
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	return nil, fmt.Errorf("không thể kết nối QUIC đến %s sau %d lần thử: %v", serverAddr, maxRetries, err)
}

// findMissingChunks tìm các phần tử bị thiếu trong một dãy số
// với giả định mỗi phần tử cách nhau 2 đơn vị.
func findMissingChunks(data []uint64) []uint64 {
	// Slice để lưu các phần tử bị thiếu
	var missingElements []uint64

	// Nếu data có ít hơn 2 phần tử, không thể so sánh
	if len(data) < 2 {
		return missingElements
	}

	// Bắt đầu từ phần tử thứ 2 (index 1) để so sánh với phần tử trước nó
	for i := 1; i < len(data); i++ {
		previous := data[i-1]
		current := data[i]

		// Kiểm tra xem khoảng cách có lớn hơn 2 không
		if current-previous > 2 {
			// Nếu có, bắt đầu một vòng lặp để tìm tất cả các số bị thiếu
			// Bắt đầu từ số (previous + 2)
			expectedNum := previous + 2

			// Tiếp tục thêm các số bị thiếu cho đến khi bằng số 'current'
			for expectedNum < current {
				missingElements = append(missingElements, expectedNum)
				expectedNum += 2 // Tăng lên 2 cho lần tìm kiếm tiếp theo
			}
		}
	}

	return missingElements
}

// Hàm chính TestListChunks
func TestListChunks(conn quic.Connection, fileKey string) error {
	fmt.Printf("--- Testing ListChunks for: %s ---\n", fileKey)
	req := ListChunksRequest{
		Command: "ListChunksRequest",
		Payload: ListChunksPayload{
			FileKey: fileKey,
		},
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("Error marshalling JSON: %v", err)
	}
	jsonData = append(jsonData, '\n')

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return fmt.Errorf("Error opening stream: %v", err)
	}
	defer stream.Close()

	if err := writeFrameWithLength(stream, jsonData); err != nil {
		return fmt.Errorf("Error writing frame: %v", err)
	}
	responseData, err := readFrameWithLength(stream)
	if err != nil {
		return fmt.Errorf("Error reading frame: %v", err)
	}

	var resp ListChunksResponse
	if err := json.Unmarshal(responseData, &resp); err != nil {
		fmt.Println("Error unmarshalling response:", err)
		fmt.Println("Raw response:", string(responseData))
		return err
	}

	// In kết quả gốc
	fmt.Printf("Status:  %s\n", resp.Status)
	fmt.Printf("Message: %s\n", resp.Message)
	fmt.Printf("Chunks:  %v\n", resp.ChunkIndices)

	// --- PHẦN KIỂM TRA MỚI ---

	// 1. Kiểm tra độ dài (len) của data
	length := len(resp.ChunkIndices)
	fmt.Printf("Độ dài (len): %d\n", length)

	// 2. Tìm các phần tử bị thiếu
	missing := findMissingChunks(resp.ChunkIndices)

	if len(missing) > 0 {
		// In đậm và rõ ràng nếu có lỗi
		fmt.Printf("\n🔥🔥🔥 PHÁT HIỆN THIẾU %d PHẦN TỬ: %v 🔥🔥🔥\n", len(missing), missing)
	} else {
		fmt.Println("\n✅ KIỂM TRA: Dữ liệu liền mạch, không thiếu phần tử nào.")
	}
	// --- KẾT THÚC PHẦN KIỂM TRA ---

	fmt.Println("---------------------------------")
	return nil
}

// TestGetLogList (Hàm mới - API 1)
func TestGetLogList(conn quic.Connection) ([]string, error) {
	fmt.Println("--- Testing GetLogList (API 1) ---")
	req := GetLogListRequest{
		Command: "GetLogList",
		Payload: nil, // Gửi nil (Option<()>)
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("Error marshalling JSON: %v", err)
	}
	jsonData = append(jsonData, '\n')

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Error opening stream: %v", err)
	}
	defer stream.Close()

	if err := writeFrameWithLength(stream, jsonData); err != nil {
		return nil, fmt.Errorf("Error writing frame: %v", err)
	}
	responseData, err := readFrameWithLength(stream)
	if err != nil {
		return nil, fmt.Errorf("Error reading frame: %v", err)
	}

	var resp LogsListResponse
	if err := json.Unmarshal(responseData, &resp); err != nil {
		fmt.Println("Error unmarshalling response:", err)
		fmt.Println("Raw response:", string(responseData))
		return nil, err
	}

	fmt.Printf("Status:  %s\n", resp.Status)
	fmt.Printf("Message: %s\n", resp.Message)
	fmt.Printf("Available Files (%d):\n", len(resp.AvailableFiles))
	for _, f := range resp.AvailableFiles {
		fmt.Printf("  - %s\n", f)
	}
	fmt.Println("---------------------------------")

	if resp.Status != "SUCCESS" || len(resp.AvailableFiles) == 0 {
		return nil, fmt.Errorf("không tìm thấy file log nào")
	}
	return resp.AvailableFiles, nil
}
func TestGetLogContent(conn quic.Connection, fileName string, saveDir string) error {
	fmt.Printf("--- Testing GetLogContent (API 2 - file: %s) ---\n", fileName)

	req := GetLogContentRequest{
		Command: "GetLogContent",
		Payload: GetLogContentPayload{
			FileName: fileName,
		},
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("Error marshalling JSON: %v", err)
	}
	jsonData = append(jsonData, '\n')

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return fmt.Errorf("Error opening stream: %v", err)
	}
	defer stream.Close()

	if err := writeFrameWithLength(stream, jsonData); err != nil {
		return fmt.Errorf("Error writing frame: %v", err)
	}
	responseData, err := readFrameWithLength(stream)
	if err != nil {
		return fmt.Errorf("Error reading frame: %v", err)
	}

	var resp LogsContentResponse
	if err := json.Unmarshal(responseData, &resp); err != nil {
		fmt.Println("Error unmarshalling response:", err)
		fmt.Println("Raw response:", string(responseData))
		return err
	}

	fmt.Printf("Status:  %s\n", resp.Status)
	fmt.Printf("Message: %s\n", resp.Message)

	if resp.Status == "SUCCESS" && resp.LogContent != nil {
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			fmt.Printf("❌ Không thể tạo thư mục %s: %v\n", saveDir, err)
			return err
		}

		filePath := filepath.Join(saveDir, resp.LogContent.FileName)
		err := os.WriteFile(filePath, []byte(resp.LogContent.Content), 0644)
		if err != nil {
			fmt.Printf("❌ Không thể ghi file %s: %v\n", filePath, err)
			return err
		}

		fmt.Printf("✅ Đã lưu log vào file: %s\n", filePath)

	} else {
		fmt.Printf("\nCould not retrieve content for %s.\n", fileName)
	}
	return nil
}

// fetchLogsFromServer kết nối tới 1 server và tải toàn bộ log về thư mục riêng
func fetchLogsFromServer(serverAddr string, wg *sync.WaitGroup) {
	defer wg.Done()

	// Lấy port để đặt tên thư mục (vd: downloaded_logs/7081)
	port := serverAddr
	for i := len(serverAddr) - 1; i >= 0; i-- {
		if serverAddr[i] == ':' {
			port = serverAddr[i+1:]
			break
		}
	}
	saveDir := filepath.Join("./downloaded_logs", port)

	log.Printf("[%s] 🔌 Đang kết nối...", serverAddr)
	conn, err := CreateQuicConnection(serverAddr)
	if err != nil {
		log.Printf("[%s] ❌ Không thể kết nối: %v", serverAddr, err)
		return
	}
	defer conn.CloseWithError(0, "done")
	log.Printf("[%s] ✅ Đã kết nối!", serverAddr)

	// Lấy danh sách log files
	availableFiles, err := TestGetLogList(conn)
	if err != nil {
		log.Printf("[%s] ❌ Lỗi khi lấy danh sách log: %v", serverAddr, err)
		return
	}

	log.Printf("[%s] ✅ Tìm thấy %d file log. Đang tải...", serverAddr, len(availableFiles))
	for i, fileName := range availableFiles {
		if i >= 10 {
			break
		}
		if err := TestGetLogContent(conn, fileName, saveDir); err != nil {
			log.Printf("[%s] ❌ Lỗi khi tải file '%s': %v", serverAddr, fileName, err)
		}
	}
	log.Printf("[%s] 🎉 Hoàn tất! Log đã được lưu vào: %s", serverAddr, saveDir)
}

func main() {
	var wg sync.WaitGroup

	log.Printf("🚀 Bắt đầu tải log từ %d server đồng thời...", len(RUST_SERVERS))

	for _, serverAddr := range RUST_SERVERS {
		wg.Add(1)
		go fetchLogsFromServer(serverAddr, &wg)
	}

	wg.Wait()
	log.Println("✅ Đã tải xong log từ tất cả các server!")
}
