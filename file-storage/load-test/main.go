package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"tool-test/file-storage/up-down-debug/config"
	"tool-test/file-storage/up-down-debug/contract"
	"tool-test/file-storage/up-down-debug/listener"
	processor "tool-test/file-storage/up-down-debug/proccessor"
	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"

	tx_models "tool-test/pkg/client-tcp/models"
	"tool-test/pkg/client-tcp/utils/tx_helper"
	mt_common "tool-test/pkg/common"
	pb "tool-test/pkg/proto"
	mt_transaction "tool-test/pkg/transaction"

	"github.com/gorilla/websocket"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"google.golang.org/protobuf/proto"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"tool-test/pkg/loggerfile"
	"github.com/quic-go/quic-go"
	"golang.org/x/net/http2"
)

type KeyEntry struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func calculateNextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	return int(math.Pow(2, math.Ceil(math.Log2(float64(n)))))
}

// buildMerkleTreePadded xây dựng cây tương thích với logic xác minh `index >> level`
// Nó trả về các "lá" đã được đệm và gốc cây
func buildMerkleTreePadded(chunks [][]byte) ([][]byte, []byte) {
	numLeaves := len(chunks)
	if numLeaves == 0 {
		emptyHash := crypto.Keccak256Hash(nil)
		return nil, emptyHash[:]
	}
	// 1. Đệm (pad) số lá lên lũy thừa của 2
	nextPowerOfTwo := calculateNextPowerOfTwo(numLeaves)
	leaves := make([][]byte, nextPowerOfTwo)
	// Hash các chunk thật
	for i := 0; i < numLeaves; i++ {
		hash := crypto.Keccak256Hash(chunks[i])
		leaves[i] = hash[:]
	}
	// Đệm phần còn lại bằng hash rỗng
	// (Lưu ý: Bạn có thể dùng một giá trị hash cố định, nhưng hash rỗng là phổ biến)
	emptyHash := crypto.Keccak256Hash([]byte{})
	for i := numLeaves; i < nextPowerOfTwo; i++ {
		leaves[i] = emptyHash[:]
	}

	// 2. Xây dựng cây
	treeLevel := leaves
	for len(treeLevel) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(treeLevel); i += 2 {
			combined := append(treeLevel[i], treeLevel[i+1]...)
			hash := crypto.Keccak256(combined)
			nextLevel = append(nextLevel, hash)
		}
		treeLevel = nextLevel
	}

	return leaves, treeLevel[0] // Trả về các lá đã hash và gốc (root)
}

// getMerkleProofPadded lấy bằng chứng cho một lá trong cây đã đệm
func getMerkleProofPadded(paddedLeaves [][]byte, chunkIndex int) [][32]byte {
	var proof [][32]byte
	treeLevel := paddedLeaves
	currentIndex := chunkIndex

	for len(treeLevel) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(treeLevel); i += 2 {
			var siblingIndex int
			if currentIndex == i {
				siblingIndex = i + 1 // Sibling bên phải
			} else if currentIndex == i+1 {
				siblingIndex = i // Sibling bên trái
			} else {
				// Không liên quan, chỉ cần hash và đi tiếp
				combined := append(treeLevel[i], treeLevel[i+1]...)
				hash := crypto.Keccak256(combined)
				nextLevel = append(nextLevel, hash)
				continue
			}

			// Đây là cấp độ chứa node của chúng ta, lấy sibling
			var sibling32 [32]byte
			copy(sibling32[:], treeLevel[siblingIndex])
			proof = append(proof, sibling32)

			// Tính hash cha
			combined := append(treeLevel[i], treeLevel[i+1]...)
			hash := crypto.Keccak256(combined)
			nextLevel = append(nextLevel, hash)
		}
		treeLevel = nextLevel
		currentIndex = currentIndex / 2 // Di chuyển lên cấp độ cha
	}

	return proof
}

// Hàm mới để chạy trong nền, duy trì kết nối
func startKeepAlive(ctx context.Context, client *ethclient.Client) {
	ticker := time.NewTicker(20 * time.Second)
	// Đảm bảo ticker được dừng khi hàm kết thúc để giải phóng tài nguyên
	defer ticker.Stop()
	log.Println("🔧 Bắt đầu goroutine duy trì kết nối (keep-alive)...")
	for {
		select {
		case <-ticker.C: // Mỗi khi ticker tick
			// Gọi một phương thức nhẹ nhàng như ChainID để gửi dữ liệu qua socket
			_, err := client.ChainID(ctx)
			if err != nil {
				// Nếu có lỗi, có thể kết nối đã bị mất
				log.Printf("⚠️ Lỗi trong quá trình keep-alive (ChainID): %v. Đang cố gắng kết nối lại...", err)
			}
		case <-ctx.Done(): // Nếu context nhận được tín hiệu hủy
			log.Println("🛑 Dừng goroutine duy trì kết nối.")
			return // Thoát khỏi vòng lặp và kết thúc goroutine
		}
	}
}
func init() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	websocket.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}

func main() {
	// --- 1. Kết nối đến client Ethereum ---
	envFile := flag.String("envfile", ".env.1", "Path to .env file")
	downloadKeyHex := flag.String("download", "", "FileKey (hex string) to download directly")
	mode := flag.String("mode", "http-bls", "Mode to use for UploadChunk: http, tcp, or http-bls")
	fileSizeGB := flag.Float64("size", 0, "Size in GB to generate a dummy file for upload")
	workers := flag.Int("workers", 10, "Number of concurrent workers for uploading chunks")
	rounds := flag.Int("rounds", 1, "Number of benchmark rounds to run sequentially")
	clientsCount := flag.Int("clients", 1, "Number of concurrent clients (reads from generated_keys.json)")
	startClient := flag.Int("startClient", 0, "Index of the first client key to use")
	isPublic := flag.Bool("public", false, "Set the uploaded file as public so anyone can download")
	flag.Parse()
	config.Load(*envFile)
	if *fileSizeGB > 0 {
		genFilePath := "./benchmark_file.bin"
		sizeBytes := int64(*fileSizeGB * 1024 * 1024 * 1024)
		f, err := os.Create(genFilePath)
		if err != nil {
			log.Fatalf("Lỗi tạo file benchmark: %v", err)
		}
		if err := f.Truncate(sizeBytes); err != nil {
			log.Fatalf("Lỗi truncate file: %v", err)
		}
		f.Close()
		config.FilePath = genFilePath
		fmt.Printf("✅ Đã tạo file benchmark %s với kích thước %.2f GB\n", genFilePath, *fileSizeGB)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Đảm bảo hàm cancel được gọi khi main thoát

	// Sử dụng rpc.DialOptions để bỏ qua xác minh SSL cho Websocket
	wsDialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	rpcWsClient, err := rpc.DialOptions(ctx, config.RpcUrl, rpc.WithWebsocketDialer(wsDialer))
	if err != nil {
		log.Fatalf("Lỗi kết nối RPC WebSocket: %v", err)
	}
	client := ethclient.NewClient(rpcWsClient)
	defer client.Close()
	fmt.Println("✅ Đã kết nối đến Ethereum client.")
	go startKeepAlive(ctx, client)
	// --- 2. Tải danh sách khóa từ generated_keys.json ---
	keysData, err := os.ReadFile("../../test_tps/gen_spam_keys/generated_keys.json")
	if err != nil {
		log.Fatalf("Lỗi đọc generated_keys.json: %v", err)
	}
	var keys []KeyEntry
	if err := json.Unmarshal(keysData, &keys); err != nil {
		log.Fatalf("Lỗi parse JSON: %v", err)
	}
	if len(keys) < *startClient+*clientsCount {
		log.Fatalf("Số lượng keys trong file (%d) ít hơn yêu cầu (%d clients từ index %d)", len(keys), *clientsCount, *startClient)
	}
	fmt.Printf("Đã tải %d khóa từ file JSON.\n", len(keys))

	// --- 3. Tải hoặc khởi tạo contract ---
	contractAddress := common.HexToAddress(config.ContractAddressHex)
	instanceWS, err := contract.NewFileContract(contractAddress, client)
	if err != nil {
		log.Fatalf("Lỗi khởi tạo contract: %v", err)
	}
	ls := listener.NewEventListener(instanceWS)
	ls.Start()
	// --- Default HTTP Client for testing bottleneck ---
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	// customTransport.MaxIdleConns = 1000
	// customTransport.MaxIdleConnsPerHost = 1000
	// customTransport.MaxConnsPerHost = 1000
	// customTransport.IdleConnTimeout = 90 * time.Second

	// Bắt buộc kích hoạt HTTP/2 (rất quan trọng khi dùng InsecureSkipVerify)
	http2.ConfigureTransport(customTransport)

	httpClient := &http.Client{
		Transport: customTransport,
		Timeout:   2 * time.Minute,
	}
	rpcClient, err := rpc.DialHTTPWithClient(config.HttpUrl, httpClient)
	if err != nil {
		log.Fatalf("Lỗi tạo RPC client: %v", err)
	}
	clientHttp := ethclient.NewClient(rpcClient)
	defer clientHttp.Close()
	instanceHttp, err := contract.NewFileContract(contractAddress, clientHttp)
	if err != nil {
		log.Fatalf("Lỗi khởi tạo contract: %v", err)
	}

	if *downloadKeyHex != "" {
		// --- CHẾ ĐỘ DOWNLOAD ---
		bytes, err := hex.DecodeString(strings.TrimPrefix(*downloadKeyHex, "0x"))
		if err != nil {
			log.Fatalf("Lỗi decode fileKey hex: %v", err)
		}
		if len(bytes) != 32 {
			log.Fatalf("FileKey phải đúng 32 byte (64 ký tự hex)")
		}
		var fileKey [32]byte
		copy(fileKey[:], bytes)

		downSummaryLogger, _ := loggerfile.NewFileLogger("logs/DownloadBenchmark_Summary.log")
		downSummaryLogger.Info("🚀 Bắt đầu DOWNLOAD với %d workers, %d clients, FileKey: %s", *workers, *clientsCount, *downloadKeyHex)
		fmt.Printf("\n🚀 Bắt đầu DOWNLOAD với %d workers, %d clients\n", *workers, *clientsCount)

		var wgClients sync.WaitGroup
		var mu sync.Mutex
		var totalBytesAll uint64   // tổng bytes tải xuống thực tế
		var sumAvgMbps float64     // tổng tốc độ trung bình từng client (để tính avg/client)
		var totalSuccessAll int
		var maxWallTime time.Duration // thời gian wall-clock dài nhất (client chậm nhất)

		wallStart := time.Now()

		for c := *startClient; c < *startClient+*clientsCount; c++ {
			wgClients.Add(1)
			go func(cIndex int) {
				defer wgClients.Done()

				clientPrivHex := keys[cIndex].PrivateKey
				clientPriv, _ := crypto.HexToECDSA(clientPrivHex)

				downLogger, _ := loggerfile.NewFileLogger(fmt.Sprintf("logs/DownloadFile_Client%d.log", cIndex))
				downLogger.Info("========================================")
				downLogger.Info("🚀 Bắt đầu DOWNLOAD | Client %d | FileKey: %s | Workers: %d", cIndex, *downloadKeyHex, *workers)

				totalRounds := *rounds
				var sumMbps float64
				var successRounds int
				var clientTotalBytes uint64

				for r := 1; r <= totalRounds; r++ {
					roundMsg := fmt.Sprintf("🔄 [Client %d] DOWNLOAD ROUND %d / %d", cIndex, r, totalRounds)
					fmt.Printf("\n%s\n", roundMsg)
					downLogger.Info(roundMsg)

					startChunk := time.Now()

					contentLen, durBlockchain, durQUIC, err := DownloadFile(client, clientPriv, instanceHttp, fileKey, *workers, cIndex)
					if err != nil {
						errMsg := fmt.Sprintf("❌ [Client %d] Lỗi tải xuống round %d: %v", cIndex, r, err)
						log.Print(errMsg)
						downLogger.Info(errMsg)
						continue
					}

					totalDuration := time.Since(startChunk)
					totalMbps := (float64(contentLen) / (1024 * 1024)) / totalDuration.Seconds()
					sumMbps += totalMbps
					clientTotalBytes += contentLen
					successRounds++

					logMsg := fmt.Sprintf("[Client %d] [Round %d] 📥 Hoàn tất. Time: %.2fs (Chain: %.2fs, QUIC: %.2fs). Tốc độ: %.2f MB/s",
						cIndex, r, totalDuration.Seconds(), durBlockchain.Seconds(), durQUIC.Seconds(), totalMbps)
					fmt.Println(logMsg)
					downLogger.Info(logMsg)
				}

				var avgMbps float64
				if successRounds > 0 {
					avgMbps = sumMbps / float64(successRounds)
				}
				summaryMsg := fmt.Sprintf("[Client %d] ✅ Hoàn tất %d/%d rounds. Tốc độ trung bình: %.2f MB/s",
					cIndex, successRounds, totalRounds, avgMbps)
				downLogger.Info(summaryMsg)
				downSummaryLogger.Info(summaryMsg)
				fmt.Println(summaryMsg)

				mu.Lock()
				sumAvgMbps += avgMbps
				totalSuccessAll += successRounds
				totalBytesAll += clientTotalBytes
				elapsed := time.Since(wallStart)
				if elapsed > maxWallTime {
					maxWallTime = elapsed
				}
				mu.Unlock()
			}(c)
		}
		wgClients.Wait()

		// Throughput thực: tổng bytes / thời gian wall-clock
		wallTotal := time.Since(wallStart)
		trueAggregateMbps := (float64(totalBytesAll) / (1024 * 1024)) / wallTotal.Seconds()
		avgPerClient := sumAvgMbps / float64(*clientsCount)

		overall := fmt.Sprintf(
			"\n📊 TỔNG KẾT DOWNLOAD:\n"+
				"   Clients          : %d\n"+
				"   Tổng dữ liệu     : %.2f MB\n"+
				"   Thời gian thực   : %.2fs\n"+
				"   Tốc độ avg/client: %.2f MB/s  ← trung bình mỗi client tải được bao nhiêu\n"+
				"   Throughput server : %.2f MB/s  ← server phải phục vụ bao nhiêu MB/s tổng",
			*clientsCount,
			float64(totalBytesAll)/(1024*1024),
			wallTotal.Seconds(),
			avgPerClient,
			trueAggregateMbps,
		)
		downSummaryLogger.Info(overall)
		fmt.Println(overall)
		return
	}
	// --- CHẾ ĐỘ UPLOAD ---
	summaryLogger, _ := loggerfile.NewFileLogger("UploadBenchmark_Summary.log")
	summaryLogger.Info("🚀 Bắt đầu UPLOAD với %d workers (mode: %s), %d clients", *workers, *mode, *clientsCount)
	fmt.Printf("\n🚀 Bắt đầu UPLOAD với %d workers (mode: %s), %d clients\n", *workers, *mode, *clientsCount)

	var wgClients sync.WaitGroup
	for c := *startClient; c < *startClient+*clientsCount; c++ {
		wgClients.Add(1)
		go func(cIndex int) {
			defer wgClients.Done()

			clientPrivHex := keys[cIndex].PrivateKey
			clientPriv, _ := crypto.HexToECDSA(clientPrivHex)
			publicKey := clientPriv.Public()
			publicKeyECDSA, _ := publicKey.(*ecdsa.PublicKey)
			fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

			upLogger, _ := loggerfile.NewFileLogger(fmt.Sprintf("UploadFile_Client%d.log", cIndex))
			totalRounds := *rounds
			var totalMbps float64
			var successRounds int

			var tcpClient *client_tcp.Client
			var tcpCfg *tcp_config.ClientConfig
			var contractABI *abi.ABI
			var err error
			contractABI, err = contract.FileContractMetaData.GetAbi()
			if err != nil {
				log.Fatalf("Failed to get contract ABI: %v", err)
			}

			if *mode == "tcp" {
				tcpCfg = &tcp_config.ClientConfig{
					ParentConnectionAddress: config.TcpUrl,
					PrivateKey_:             config.PrivateKeyBLS,
					EthPrivateKey:           clientPrivHex,
					ChainId:                 uint64(config.ChainId),
					ParentAddress:           fromAddress.Hex(),
				}
				tcpClient, err = client_tcp.NewClient(tcpCfg)
				if err != nil {
					log.Printf("Lỗi kết nối TCP client %d: %v", cIndex, err)
					return
				}
			}

			for round := 1; round <= totalRounds; round++ {
				startChunk := time.Now()
				fileKey, _, contentLen, chunkUploadDuration := uploadFile(client, clientHttp, clientPriv, instanceHttp, fromAddress, *mode, *workers, tcpClient, tcpCfg, contractABI, cIndex, upLogger, *isPublic)

				listener.UploadStartTimes.Store(fileKey, startChunk)

				sentTime := time.Now()
				listener.UploadEndTimes.Store(fileKey, sentTime)

				totalElapsedSecs := sentTime.Sub(startChunk).Seconds()
				mbps := (float64(contentLen) / (1024 * 1024)) / totalElapsedSecs

				totalMbps += mbps
				successRounds++

				upLogger.Info("[%s] [Round %d] 📤 Hoàn tất. Time: %vs (Chunk: %vs). Tốc độ: %.2f MB/s",
					*mode, round, totalElapsedSecs, chunkUploadDuration.Seconds(), mbps)

				summaryLogger.Info("🎉 [Client %d] Upload thành công! FileKey: %x (%.2f MB/s)", cIndex, fileKey, mbps)
				fmt.Printf("🎉 [Client %d] Upload thành công! FileKey: %x (%.2f MB/s)\n", cIndex, fileKey, mbps)
			}

		}(c)
	}
	wgClients.Wait()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("--- Nhận được tín hiệu dừng.. ---")
}
func uploadFileGetInputData(client *ethclient.Client, privateKey *ecdsa.PrivateKey, instance *contract.FileContract, fromAddress common.Address) ([32]byte, string) {
	fileData, err := os.ReadFile(config.FilePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	fileName := filepath.Base(config.FilePath)
	fileExt := filepath.Ext(fileName)
	contentLen := uint64(len(fileData))
	totalChunks := (contentLen + config.ChunkSize - 1) / config.ChunkSize
	fmt.Println("Building PADDED Merkle Tree (compatible with server logic)...")
	var chunks [][]byte
	for i := uint64(0); i < totalChunks; i++ {
		start := i * config.ChunkSize
		end := start + config.ChunkSize
		if end > contentLen {
			end = contentLen
		}
		chunkData := fileData[start:end]
		chunks = append(chunks, chunkData)
	}
	// 3. Lấy Merkle Root
	paddedLeaves, merkleRoot := buildMerkleTreePadded(chunks)
	var merkleRoot32 [32]byte
	copy(merkleRoot32[:], merkleRoot)

	fmt.Printf("Uploading File:\n  Name: %s\n  Size: %d bytes\n  Total Chunks: %d\n  Merkle Root: %x\n",
		fileName, contentLen, totalChunks, merkleRoot32)

	info := contract.Info{
		Owner:              fromAddress,
		MerkleRoot:         merkleRoot32,
		ContentLen:         contentLen,
		TotalChunks:        totalChunks,
		ExpireTime:         uint64(time.Now().Add(365 * 24 * time.Hour).Unix()),
		Name:               fileName,
		Ext:                fileExt,
		ContentDisposition: "inline",
		ContentID:          fmt.Sprintf("%x", merkleRoot32),
	}
	log.Printf("File info prepared: %+v\n", info)
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

	requiredPayment, err := instance.CalculatePrice(&bind.CallOpts{}, big.NewInt(int64(totalChunks)))
	if err != nil {
		log.Fatalf("Failed to calculate price: %v", err)
	}
	fmt.Printf("Required payment: %s wei (%.6f ETH)\n", requiredPayment.String(), float64(requiredPayment.Int64())/1e18)

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create transactor: %v", err)
	}
	auth.GasLimit = uint64(3_000_000_0000000)
	auth.GasPrice, _ = client.SuggestGasPrice(context.Background())
	auth.Value = requiredPayment // Gửi kèm thanh toán

	tx, err := instance.PushFileInfo(auth, info)
	if err != nil {
		log.Fatalf("Failed to call PushFileInfo: %v", err)
	}

	fmt.Println("Waiting for PushFileInfo tx to be mined...")
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		log.Fatalf("Failed to wait for tx: %v", err)
	}
	log.Printf("PushFileInfo tx %v mined in block %d with status %d", tx.Hash(), receipt.BlockNumber.Uint64(), receipt.Status)
	var fileKey [32]byte
	for _, v := range receipt.Logs {
		if parsed, err := instance.ParseFileAdded(*v); err == nil {
			fileKey = parsed.FileKey
			fmt.Printf("FileKey successfully retrieved: %x\n", fileKey)
			break
		}
	}
	if fileKey == [32]byte{} {
		log.Fatal("FileKey not found in logs")
	}

	// 🔥 Lấy ABI để encode inputData
	contractABI, err := contract.FileContractMetaData.GetAbi()
	if err != nil {
		log.Fatalf("Failed to get contract ABI: %v", err)
	}

	// 🔥 Tạo InputData cho từng chunk
	fmt.Println("\n🔍 Generating InputData for each chunk...")
	for i := uint64(0); i < totalChunks; i++ {
		start := i * config.ChunkSize
		end := start + config.ChunkSize
		if end > contentLen {
			end = contentLen
		}
		chunk := fileData[start:end]
		proofBytes := getMerkleProofPadded(paddedLeaves, int(i))

		// Encode inputData cho UploadChunk
		inputData, err := contractABI.Pack("uploadChunk", fileKey, chunk, big.NewInt(int64(i)), proofBytes)
		if err != nil {
			log.Printf("❌ Failed to encode chunk %d: %v", i, err)
			continue
		}

		fmt.Printf("\n📦 Chunk %d InputData:\n", i)
		fmt.Printf("   Length: %d bytes\n", len(inputData))

		// Lưu inputData của từng chunk vào file riêng
		filename := fmt.Sprintf("upload_chunk_%d_inputdata.txt", i)
		err = os.WriteFile(filename, []byte(hex.EncodeToString(inputData)), 0644)
		if err != nil {
			log.Printf("⚠️  Failed to write chunk %d inputData to file: %v", i, err)
		} else {
			fmt.Printf("   ✅ Saved to: %s\n", filename)
		}
	}

	fmt.Printf("\n🎉 All InputData generated for %d chunks!\n", totalChunks)
	return fileKey, fileName
}

func uploadFile(client *ethclient.Client, clientHttp *ethclient.Client, privateKey *ecdsa.PrivateKey, instance *contract.FileContract, fromAddress common.Address, mode string, numWorkers int, tcpClient *client_tcp.Client, tcpCfg *tcp_config.ClientConfig, contractABI *abi.ABI, clientID int, upLogger *loggerfile.FileLogger, isPublic bool) ([32]byte, string, uint64, time.Duration) {
	fileData, err := os.ReadFile(config.FilePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	fileName := filepath.Base(config.FilePath)
	fileExt := filepath.Ext(fileName)
	contentLen := uint64(len(fileData))
	totalChunks := (contentLen + config.ChunkSize - 1) / config.ChunkSize
	upLogger.Info("Building PADDED Merkle Tree (compatible with server logic)...")
	var chunks [][]byte
	for i := uint64(0); i < totalChunks; i++ {
		start := i * config.ChunkSize
		end := start + config.ChunkSize
		if end > contentLen {
			end = contentLen
		}
		chunkData := fileData[start:end]
		chunks = append(chunks, chunkData)
		// merkleLeaves = append(merkleLeaves, MerkleContent{data: chunkData}) // BỎ DÒNG NÀY
	}
	// 3. Lấy Merkle Root
	paddedLeaves, merkleRoot := buildMerkleTreePadded(chunks)
	var merkleRoot32 [32]byte
	copy(merkleRoot32[:], merkleRoot)

	upLogger.Info("Uploading File: Name: %s, Size: %d bytes, Total Chunks: %d, Merkle Root: %x",
		fileName, contentLen, totalChunks, merkleRoot32)

	info := contract.Info{
		Owner:              fromAddress,
		MerkleRoot:         merkleRoot32,
		ContentLen:         contentLen,
		TotalChunks:        totalChunks,
		ExpireTime:         uint64(time.Now().Add(365 * 24 * time.Hour).Unix()),
		Name:               fileName,
		Ext:                fileExt,
		ContentDisposition: "inline",
		ContentID:          fmt.Sprintf("%x", merkleRoot32),
	}
	log.Printf("File info prepared: %+v\n", info)
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

	requiredPayment, err := instance.CalculatePrice(&bind.CallOpts{}, big.NewInt(int64(totalChunks)))
	if err != nil {
		log.Fatalf("Failed to calculate price: %v", err)
	}
	upLogger.Info("Required payment: %s wei (%.6f ETH)", requiredPayment.String(), float64(requiredPayment.Int64())/1e18)

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create transactor: %v", err)
	}
	auth.GasLimit = uint64(3_000_000_0000000)
	auth.GasPrice, _ = client.SuggestGasPrice(context.Background())
	auth.Value = requiredPayment // Gửi kèm thanh toán

	tx, err := instance.PushFileInfo(auth, info)
	if err != nil {
		log.Fatalf("Failed to call PushFileInfo: %v", err)
	}

	upLogger.Info("Waiting for PushFileInfo tx to be mined...")
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		log.Fatalf("Failed to wait for tx: %v", err)
	}
	upLogger.Info("PushFileInfo tx %v mined in block %d with status %v", tx.Hash(), receipt.BlockNumber.Uint64(), receipt.Status)
	var fileKey [32]byte
	for _, v := range receipt.Logs {
		if parsed, err := instance.ParseFileAdded(*v); err == nil {
			// Chỉ cần truy cập vào trường 'FileKey' của struct trả về là được
			fileKey = parsed.FileKey
			upLogger.Info("FileKey successfully retrieved: %x", fileKey)
			break // Thoát khỏi vòng lặp khi đã tìm thấy
		}
	}
	if fileKey == [32]byte{} {
		log.Fatal("FileKey not found in logs")
	}

	if isPublic {
		upLogger.Info("Setting file %x to public...", fileKey)
		auth.Value = big.NewInt(0)
		txPub, errPub := instance.SetPublicStatus(auth, fileKey, true)
		if errPub != nil {
			log.Printf("⚠️ Failed to set public status: %v", errPub)
		} else {
			upLogger.Info("SetPublicStatus tx %s sent. Waiting to be mined...", txPub.Hash().Hex())
			_, errWait := bind.WaitMined(context.Background(), client, txPub)
			if errWait != nil {
				log.Printf("⚠️ Failed to wait for SetPublicStatus tx: %v", errWait)
			} else {
				upLogger.Info("File is now PUBLIC!")
			}
		}
	}

	var countErr int
	sem := make(chan struct{}, numWorkers) // Tăng limit lên số lượng workers
	var wg sync.WaitGroup
	auth.Value = big.NewInt(0) // Chỉ cần thanh toán một lần ban đầu

	chunkUploadStartTime := time.Now()

	for i := uint64(0); i < totalChunks; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i uint64) {
			defer wg.Done()
			defer func() { <-sem }() // Đảm bảo semaphore được giải phóng
			// --- Bước 1: Chuẩn bị dữ liệu (chỉ làm 1 lần) ---
			start := i * config.ChunkSize
			end := start + config.ChunkSize
			if end > contentLen {
				end = contentLen
			}
			chunk := fileData[start:end]

			// Tạo Merkle Proof
			proofBytes := getMerkleProofPadded(paddedLeaves, int(i))

			inputData, err := contractABI.Pack("uploadChunk", fileKey, chunk, big.NewInt(int64(i)), proofBytes)
			if err != nil {
				log.Printf("❌ Failed to encode chunk %d: %v", i, err)
				countErr++
				return
			}

			// --- Bước 2: Logic Thử lại (Retry) ---
			const maxRetries = 3               // Thử lại tối đa 3 lần
			const retryDelay = 1 * time.Second // Chờ 3 giây giữa các lần thử
			for attempt := 1; attempt <= maxRetries; attempt++ {
				startChunk := time.Now()

				switch mode {
				case "http-bls":
					upLogger.Info("🚀 [Chunk %d -k %s] up (HTTP BLS)...", i, hex.EncodeToString(fileKey[:]))
					callData := mt_transaction.NewCallData(inputData)
					var payload []byte
					payload, err = callData.Marshal()
					if err != nil {
						log.Printf("❌ Failed to marshal calldata for chunk %d: %v", i, err)
						countErr++
						return
					}
					tx := mt_transaction.NewTransactionWithoutNonce(
						fromAddress,
						common.HexToAddress(config.ContractAddressHex),
						big.NewInt(0),
						5_000_000,
						20_000_000_000,
						uint64(time.Now().Unix()+300),
						payload,
						[][]byte{},
						common.Hash{},
						common.Hash{},
						uint64(config.ChainId),
					)
					blsPrivBytes, _ := hex.DecodeString(strings.TrimPrefix(config.PrivateKeyBLS, "0x"))
					tx.SetSign(mt_common.PrivateKeyFromBytes(blsPrivBytes))

					txWithDeviceKey := &pb.TransactionWithDeviceKey{
						Transaction: tx.Proto().(*pb.Transaction),
						DeviceKey:   []byte{},
					}
					bTx, errMarshal := proto.Marshal(txWithDeviceKey)
					if errMarshal != nil {
						log.Printf("❌ Failed to marshal tx: %v", errMarshal)
						return
					}

					var txHashStr string
					errTCP := clientHttp.Client().CallContext(context.Background(), &txHashStr, "mtn_sendRawTransactionWithDeviceKey", hexutil.Bytes(bTx))
					if errTCP == nil {
						txHash := common.HexToHash(txHashStr)
						sentTime := time.Now()
						upLogger.Info("📤 [Chunk %d -k %s] up xong HTTP BLS: tx=%s (mất %s để gửi)",
							i, hex.EncodeToString(fileKey[:]), txHash.Hex(), sentTime.Format("15:04:05.000"), sentTime.Sub(startChunk))
						return
					}
					err = errTCP

				case "tcp":
					upLogger.Info("🚀 [Chunk %d -k %s] up (TCP)...", i, hex.EncodeToString(fileKey[:]))
					txHash, errTCP := tx_helper.SendTransactionAsync(
						"uploadChunk",
						tcpClient,
						tcpCfg,
						common.HexToAddress(config.ContractAddressHex),
						fromAddress,
						inputData,
						&tx_models.TxOptions{
							MaxGas:      5_000_000,
							MaxGasPrice: 20_000_000_000,
						},
					)
					if errTCP == nil {
						sentTime := time.Now()
						upLogger.Info("📤 [Chunk %d -k %s] up xong TCP: tx=%s (mất %s để gửi)",
							i, hex.EncodeToString(fileKey[:]), txHash.Hex(), sentTime.Format("15:04:05.000"), sentTime.Sub(startChunk))
						return
					}
					err = errTCP

				default: // "http"
					upLogger.Info("🚀 [Chunk %d -k %s] up (HTTP)...", i, hex.EncodeToString(fileKey[:]))
					var txRPC *types.Transaction
					txRPC, err = instance.UploadChunk(auth, fileKey, chunk, big.NewInt(int64(i)), proofBytes)
					if err == nil {
						// THÀNH CÔNG
						sentTime := time.Now()
						if txRPC != nil {
							upLogger.Info("📤 [Chunk %d -k %s] up xong HTTP: tx=%s (mất %s để gửi)",
								i, hex.EncodeToString(fileKey[:]), txRPC.Hash().Hex(), sentTime.Format("15:04:05.000"), sentTime.Sub(startChunk))
						}
						return // Thoát khỏi goroutine (thành công)
					}
				}

				// THẤT BẠI
				if err != nil && strings.Contains(err.Error(), "to store chunk on disk") {
					return
				}
				log.Printf("⚠️ [Chunk %d, Thử %d/%d] Lỗi UploadChunk: %v", i, attempt, maxRetries, err)
				upLogger.Info("⚠️ [Chunk %d, Thử %d/%d] Lỗi UploadChunk: %v", i, attempt, maxRetries, err)
				if attempt < maxRetries {
					log.Printf("... [Chunk %d] Sẽ thử lại sau %v...", i, retryDelay)
					upLogger.Info("... [Chunk %d] Sẽ thử lại sau %v...", i, retryDelay)
					time.Sleep(retryDelay)
				}
			}

			log.Printf("❌ [Chunk %d] TỪ BỎ sau %d lần thử. Lỗi cuối: %v", i, maxRetries, err)
			upLogger.Info("❌ [Chunk %d] TỪ BỎ sau %d lần thử. Lỗi cuối: %v", i, maxRetries, err)
			countErr++
		}(i)
	}
	wg.Wait()
	chunkUploadDuration := time.Since(chunkUploadStartTime)
	upLogger.Info("✅ File upload completed! với %d lỗi", countErr)
	return fileKey, fileName, contentLen, chunkUploadDuration
}

// DownloadFile là một hàm độc lập để tải tệp bằng fileKey của nó. Trả về: kích thước tệp, thời gian Blockchain, thời gian QUIC download, lỗi.
func DownloadFile(client *ethclient.Client, privateKey *ecdsa.PrivateKey, instance *contract.FileContract, fileKey [32]byte, workers int, clientID int) (uint64, time.Duration, time.Duration, error) {
	fileKeyHex := hex.EncodeToString(fileKey[:])

	// Khởi tạo logger để lưu kết quả ra file DownloadBenchmark.log
	downLogger, _ := loggerfile.NewFileLogger("DownloadBenchmark.log")
	downLogger.Info("========================================")
	downLogger.Info("🚀 Bắt đầu quá trình tải tệp với FileKey: %s | Workers: %d", fileKeyHex, workers)
	fmt.Printf("\nBắt đầu quá trình tải tệp với FileKey: %s\n", fileKeyHex)

	// --- Bước 1: Lấy thông tin tệp từ Blockchain ---
	fmt.Println("\nBước 1: Đang lấy thông tin tệp (GetFileInfo)...")
	tGetInfo := time.Now()
	fileInfo, err := instance.GetFileInfo(&bind.CallOpts{}, fileKey)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi lấy thông tin tệp: %v", err)
	}
	if fileInfo.ContentLen == 0 {
		return 0, 0, 0, fmt.Errorf("không tìm thấy thông tin cho FileKey này hoặc tệp không có nội dung")
	}
	durGetInfo := time.Since(tGetInfo)
	msgInfo := fmt.Sprintf("--- Thông tin tệp từ Blockchain ---\n  Tên: %s\n  Kích thước: %d bytes\n  Hash (SHA256): %x\n  Tổng số chunk: %d\n",
		fileInfo.Name, fileInfo.ContentLen, fileInfo.MerkleRoot, fileInfo.TotalChunks)
	fmt.Print(msgInfo)
	downLogger.Info(msgInfo)

	msgGetInfoTime := fmt.Sprintf("⏱️ Thời gian lấy GetFileInfo: %v", durGetInfo)
	fmt.Println(msgGetInfoTime)
	downLogger.Info(msgGetInfoTime)

	// --- Bước 2: Thanh toán và lấy DownloadKey ---
	fmt.Println("\nBước 2: Đang thanh toán để tải xuống và lấy DownloadKey...")
	tPay := time.Now()
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi lấy chain ID: %v", err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tạo transactor: %v", err)
	}

	requiredPayment, err := instance.CalculatePrice(&bind.CallOpts{}, big.NewInt(int64(fileInfo.TotalChunks)))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tính toán giá: %v", err)
	}
	fmt.Printf("Yêu cầu thanh toán để tải xuống: %s wei (%.6f ETH)\n", requiredPayment.String(), float64(requiredPayment.Int64())/1e18)

	auth.GasLimit = uint64(3_000_000)
	auth.GasPrice, _ = client.SuggestGasPrice(context.Background())
	auth.Value = requiredPayment // Gửi kèm thanh toán

	tx, err := instance.PayForDownload(auth, fileKey, big.NewInt(1))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi thanh toán cho việc tải tệp: %v", err)
	}
	fmt.Printf("Giao dịch thanh toán đã được gửi: %s. Đang chờ khai thác...\n", tx.Hash().Hex())

	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi chờ giao dịch được khai thác: %v", err)
	}
	if receipt.Status != 1 {
		return 0, 0, 0, fmt.Errorf("giao dịch thanh toán thất bại. Trạng thái: %d", receipt.Status)
	}

	var downloadKey [32]byte
	var foundEvent bool
	for _, vLog := range receipt.Logs {
		parsedLog, err := instance.ParseDownloadKeyGenerated(*vLog)
		if err == nil { // Nếu parse thành công, đây chính là sự kiện chúng ta cần
			downloadKey = parsedLog.DownloadKey
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		return 0, 0, 0, fmt.Errorf("không thể tìm thấy sự kiện DownloadKeyGenerated trong biên lai giao dịch")
	}
	durPay := time.Since(tPay)
	msgPay := fmt.Sprintf("⏱️ Thời gian PayForDownload (tạo tx + chờ khai thác + lấy key): %v", durPay)
	fmt.Println(msgPay)
	downLogger.Info(msgPay)

	// hexStr := "850721e32817d473052c1ba7a5e249379dbd3ace4f5ab82fa95373249351dc4c"
	// bytes, err := hex.DecodeString(hexStr)
	// if err != nil {
	// 	panic(err)
	// }
	// // Đảm bảo đúng 32 byte
	// if len(bytes) != 32 {
	// 	panic("Không đúng 32 byte")
	// }
	// // Chuyển thành [32]byte
	// var downloadKey [32]byte
	// copy(downloadKey[:], bytes)

	downloadKeyHex := hex.EncodeToString(downloadKey[:])
	log.Printf("Thanh toán thành công! DownloadKey nhận được: %s", downloadKeyHex)

	// --- Bước 3: Tải xuống các chunk từ Rust servers ---
	fmt.Println("\nBước 3: Đang tải xuống các chunk từ Rust servers...")
	tDownload := time.Now()
	var wg sync.WaitGroup
	// Chuẩn bị file đích trước để ghi trực tiếp (Zero-copy, O(1) RAM)
	outputFileName := fmt.Sprintf("%s.client%d", config.OutputFile, clientID)
	outputDir := filepath.Dir(outputFileName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tạo thư mục đầu ra '%s': %v", outputDir, err)
	}
	outFile, err := os.OpenFile(outputFileName, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tạo file đầu ra: %v", err)
	}
	defer outFile.Close()
	outFile.Truncate(int64(fileInfo.ContentLen)) // Cấp phát cứng ổ đĩa trước

	var downloadedCount uint64
	var mu sync.Mutex

	conn1, err := processor.CreateQuicConnection(processor.RUST_SERVER_1_ADDR_QUIC)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tạo kết nối QUIC đến server 1: %v", err)
	}
	conn2, err := processor.CreateQuicConnection(processor.RUST_SERVER_2_ADDR_QUIC)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi tạo kết nối QUIC đến server 2: %v", err)
	}
	// Ký tin nhắn MỘT LẦN duy nhất thay vì ký 2000 lần
	sign, err := listener.SignMessage(privateKey, downloadKeyHex)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lỗi ký thông điệp tải xuống: %v", err)
	}

	// Giới hạn số lượng worker tải đồng thời (theo tham số -workers)
	if workers <= 0 {
		workers = 50 // fallback default
	}
	sem := make(chan struct{}, workers)

	for i := uint64(0); i < fileInfo.TotalChunks; i++ {
		wg.Add(1)
		sem <- struct{}{} // Giới hạn worker
		go func(i uint64) {
			defer wg.Done()
			defer func() { <-sem }()

			var chunkData []byte
			var err error
			const maxRetries = 3

			for attempt := 1; attempt <= maxRetries; attempt++ {
				var conn quic.Connection
				if int(i)%2 == 0 {
					conn = conn1
				} else {
					conn = conn2
				}
				chunkData, err = processor.RequestChunkFromRustServerQuic(conn, fileKeyHex, downloadKeyHex, int(i), sign)
				if err == nil {
					break // Tải thành công
				}
				
				log.Printf("⚠️ Lỗi tải chunk %d (lần %d/%d): %v", i, attempt, maxRetries, err)
				
				// Nếu kết nối bị timeout hoặc bị đóng bởi QUIC, ta tạo một kết nối khẩn cấp cục bộ để cứu nguy cho Chunk này
				if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "closed") {
					addr := processor.RUST_SERVER_1_ADDR_QUIC
					if int(i)%2 != 0 {
						addr = processor.RUST_SERVER_2_ADDR_QUIC
					}
					log.Printf("🔄 Tạo kết nối phục hồi cho chunk %d đến %s", i, addr)
					if newConn, errConn := processor.CreateQuicConnection(addr); errConn == nil {
						chunkData, err = processor.RequestChunkFromRustServerQuic(newConn, fileKeyHex, downloadKeyHex, int(i), sign)
						if err == nil {
							break // Phục hồi thành công
						}
					}
				}
				
				time.Sleep(200 * time.Millisecond) // Đổi sang 200ms để không bị chờ quá lâu
			}

			if err != nil {
				log.Printf("❌ Bỏ cuộc tải chunk %d sau %d lần thử: %v", i, maxRetries, err)
				return 
			}

			offset := int64(i * config.ChunkSize)
			_, writeErr := outFile.WriteAt(chunkData, offset)
			if writeErr != nil {
				log.Printf("Lỗi ghi chunk %d ra ổ đĩa: %v", i, writeErr)
				return
			}

			mu.Lock()
			downloadedCount++
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	durDownload := time.Since(tDownload)
	mbps := (float64(fileInfo.ContentLen) / 1024 / 1024) / durDownload.Seconds()
	msgDownload := fmt.Sprintf("⏱️ Thời gian tải xuống song song %d chunks mạng QUIC: %v (Tốc độ mạng: %.2f MB/s)", fileInfo.TotalChunks, durDownload, mbps)
	fmt.Println(msgDownload)
	downLogger.Info(msgDownload)

	if downloadedCount != fileInfo.TotalChunks {
		errStr := fmt.Sprintf("tải tệp thất bại. Chỉ tải được %d/%d chunks", downloadedCount, fileInfo.TotalChunks)
		downLogger.Info(errStr)
		return 0, 0, 0, fmt.Errorf(errStr)
	}

	// --- Bước 4: Đồng bộ đĩa ---
	fmt.Println("\nBước 4: Đang đồng bộ đĩa (fsync)...")
	tMerge := time.Now()
	outFile.Sync() // fsync ổ đĩa

	durMerge := time.Since(tMerge)
	msgMerge := fmt.Sprintf("⏱️ Thời gian ghép chunk và ghi đĩa: %v", durMerge)
	fmt.Println(msgMerge)
	downLogger.Info(msgMerge)

	msgSuccess := fmt.Sprintf("🎉 Tệp đã được lưu thành công với tên '%s'", outputFileName)
	fmt.Println("\n" + msgSuccess)
	// downLogger.Info(msgSuccess) -> Không cần vì ta đã gộp log ở trên main()
	return fileInfo.ContentLen, (durGetInfo + durPay), durDownload, nil
}
