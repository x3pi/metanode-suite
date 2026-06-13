//go:build ignore

package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Config struct {
	EthPK        string `json:"eth_pk"`
	HttpRPC      string `json:"http_rpc"`
	WebsocketRPC string `json:"websocket_rpc"`
	ChainID      int64  `json:"chain_id"`
}

func main() {
	configFlag := flag.String("config", "user1.json", "Đường dẫn file cấu hình (chứa eth_pk, http_rpc)")
	contractFlag := flag.String("contract", "", "Địa chỉ contract SimpleChat")
	deployFlag := flag.Bool("deploy", false, "Chỉ định deploy contract mới")
	targetFlag := flag.String("target", "", "Địa chỉ người nhận tin nhắn (nếu trống sẽ tự nhận diện)")
	spamFlag := flag.Int("spam", 0, "Số lượng tin nhắn cần spam tự động")
	pingFlag := flag.Bool("ping", false, "Kích hoạt chế độ đo Ping-Pong (gửi PING, chờ PONG, đo RTT)")
	flag.Parse()

	// 1. Đọc config
	configData, err := os.ReadFile(*configFlag)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(configData, &cfg); err != nil {
		log.Fatalf("❌ Lỗi parse config: %v", err)
	}

	rpcURL := cfg.HttpRPC
	if !strings.HasPrefix(rpcURL, "http") {
		rpcURL = "http://" + rpcURL
	}

	fmt.Println("==================================================")
	fmt.Printf("🚀 START HTTP RPC CHAT CLI (-config=%s)\n", *configFlag)
	fmt.Println("==================================================")

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	privateKey, err := crypto.HexToECDSA(cfg.EthPK)
	if err != nil {
		log.Fatalf("❌ Lỗi private key: %v", err)
	}
	publicKeyECDSA, ok := privateKey.Public().(*ecdsa.PublicKey)
	if !ok {
		log.Fatalf("❌ Lỗi public key")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Printf("👤 Địa chỉ của bạn: %s\n", fromAddress.Hex())

	// 2. Load ABI và Bytecode
	abiFile, err := os.Open("SimpleChat.abi")
	if err != nil {
		log.Fatalf("❌ Lỗi mở SimpleChat.abi: %v", err)
	}
	contractAbi, err := abi.JSON(abiFile)
	abiFile.Close()
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecodeBytes, err := os.ReadFile("SimpleChat.bin")
	if err != nil {
		log.Fatalf("❌ Lỗi mở SimpleChat.bin: %v", err)
	}
	bytecodeHex := string(bytecodeBytes)
	if !strings.HasPrefix(bytecodeHex, "0x") {
		bytecodeHex = "0x" + bytecodeHex
	}
	bytecode := common.FromHex(bytecodeHex)

	var contractAddress common.Address
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		chainID = big.NewInt(cfg.ChainID)
	}

	// 3. Deploy nếu cần
	if *deployFlag && *contractFlag == "" {
		fmt.Println("▶️ Đang Deploy contract SimpleChat...")
		nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
		if err != nil {
			log.Fatalf("❌ Lỗi lấy nonce: %v", err)
		}
		gasPrice, err := client.SuggestGasPrice(context.Background())
		if err != nil {
			gasPrice = big.NewInt(2000000000)
		}

		tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, gasPrice, bytecode)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
		if err != nil {
			log.Fatalf("❌ Lỗi ký tx deploy: %v", err)
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			log.Fatalf("❌ Lỗi gửi tx deploy: %v", err)
		}

		fmt.Printf("⏳ Đợi xác nhận deploy tx: %s\n", signedTx.Hash().Hex())
		var receipt *types.Receipt
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)
			receipt, err = client.TransactionReceipt(context.Background(), signedTx.Hash())
			if receipt != nil && err == nil {
				break
			}
		}
		if receipt == nil {
			log.Fatalf("❌ Deploy timeout, không nhận được receipt")
		}
		contractAddress = crypto.CreateAddress(fromAddress, nonce)
		fmt.Printf("🎉 DEPLOY THÀNH CÔNG! Address: %s\n", contractAddress.Hex())
	} else if *contractFlag != "" {
		contractAddress = common.HexToAddress(*contractFlag)
	} else {
		log.Fatalf("❌ Cần cung cấp -contract <address> hoặc thêm cờ -deploy")
	}

	fmt.Printf("📝 Đang sử dụng Contract Chat tại: %s\n", contractAddress.Hex())

	// 4. Target Address
	targetAddress := common.Address{}
	if *targetFlag != "" {
		targetAddress = common.HexToAddress(*targetFlag)
	} else {
		if strings.Contains(*configFlag, "user1") {
			targetAddress = common.HexToAddress("0x2C71210D239D472e963a7Be8362eCBdeD5337fE6") // user2 default
		} else {
			targetAddress = common.HexToAddress("0x5e582475A504998c5631E12A5a2585D2B1911812") // user1 default
		}
	}
	fmt.Printf("🎯 Địa chỉ người nhận (Target): %s\n", targetAddress.Hex())

	// 5. Lắng nghe sự kiện qua WebSocket (thay vì polling HTTP)
	var totalLatencyMs int64
	var msgCount int64
	pongCh := make(chan struct{}, 1)

	wsURL := cfg.WebsocketRPC
	if wsURL == "" {
		wsURL = strings.Replace(rpcURL, "http://", "ws://", 1)
		wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	}

	wsClient, err := ethclient.Dial(wsURL)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối WebSocket (%s): %v", wsURL, err)
	}

	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}

	logs := make(chan types.Log)
	sub, err := wsClient.SubscribeFilterLogs(context.Background(), query, logs)
	if err != nil {
		log.Fatalf("❌ Lỗi subscribe sự kiện qua WS: %v", err)
	}

	go func() {
		for {
			select {
			case err := <-sub.Err():
				log.Fatalf("❌ Bị mất kết nối stream WS: %v", err)
			case vLog := <-logs:
				for name, event := range contractAbi.Events {
					if vLog.Topics[0] == event.ID && name == "MessageSent" {
						decoded := make(map[string]interface{})
						err := contractAbi.UnpackIntoMap(decoded, name, vLog.Data)
						if err != nil {
							continue
						}

						msgText := decoded["message"].(string)
						msgTimestamp := decoded["timestamp"].(*big.Int)

						var from, to common.Address
						topicIdx := 1
						for _, input := range event.Inputs {
							if input.Indexed && topicIdx < len(vLog.Topics) {
								if input.Name == "from" {
									from = common.BytesToAddress(vLog.Topics[topicIdx].Bytes())
								} else if input.Name == "to" {
									to = common.BytesToAddress(vLog.Topics[topicIdx].Bytes())
								}
								topicIdx++
							}
						}

						latencyMs := time.Now().UnixMilli() - msgTimestamp.Int64()

						if from.Hex() == fromAddress.Hex() {
							if !*pingFlag {
								fmt.Printf("\n[✅ Mạng đã xác nhận tin nhắn của bạn lúc %v] (Độ trễ: %d ms)\n> ", msgTimestamp, latencyMs)
							}
						} else if to.Hex() == fromAddress.Hex() {
							if strings.HasPrefix(msgText, "PING") {
								// Tự động reply PONG
								pongText := strings.Replace(msgText, "PING", "PONG", 1)
								go func(target common.Address, pText string, ts *big.Int) {
									nonce, _ := client.PendingNonceAt(context.Background(), fromAddress)
									gasPrice, _ := client.SuggestGasPrice(context.Background())
									if gasPrice == nil { gasPrice = big.NewInt(1000000) }
									
									payloadData, _ := contractAbi.Pack("sendMessage", target, pText, ts)
									tx := types.NewTransaction(nonce, contractAddress, big.NewInt(0), 5000000, gasPrice, payloadData)
									signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
									client.SendTransaction(context.Background(), signedTx)
								}(from, pongText, msgTimestamp)
							} else if strings.HasPrefix(msgText, "PONG") {
								rtt := latencyMs
								msgCount++
								totalLatencyMs += rtt
								avgRtt := totalLatencyMs / msgCount
								fmt.Printf("\n[🏓 NHẬN %s từ %s]: (RTT: %d ms | Trung bình %d round: %d ms)\n> ", msgText, from.Hex()[:8], rtt, msgCount, avgRtt)
								
								select {
								case pongCh <- struct{}{}:
								default:
								}
							} else {
								msgCount++
								totalLatencyMs += latencyMs
								avgLatency := totalLatencyMs / msgCount
								e2eLatencyStr := fmt.Sprintf(" (Độ trễ: %d ms | Trung bình %d tin: %d ms)", latencyMs, msgCount, avgLatency)
								fmt.Printf("\n[📥 NHẬN từ %s]: %s%s\n> ", from.Hex()[:8], msgText, e2eLatencyStr)
							}
						}
					}
				}
			}
		}
	}()

	fmt.Println("==================================================")
	fmt.Println("💬 CHAT HTTP RPC SẴN SÀNG! Bạn có thể gõ nội dung và nhấn Enter.")
	fmt.Println("==================================================")

	if *spamFlag > 0 {
		if *pingFlag {
			fmt.Printf("🚀 Đang tự động chơi Ping-Pong %d rounds...\n", *spamFlag)
		} else {
			fmt.Printf("🚀 Đang tự động spam %d tin nhắn...\n", *spamFlag)
		}
		nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
		if err != nil {
			log.Fatalf("❌ Lỗi lấy nonce: %v", err)
		}
		gasPrice, _ := client.SuggestGasPrice(context.Background())
		if gasPrice == nil {
			gasPrice = big.NewInt(1000000)
		}

		for i := 0; i < *spamFlag; i++ {
			longText := "Xin chào! Đây là một đoạn tin nhắn mô phỏng rất dài nhằm mục đích giả lập hành vi chat thực tế của người dùng. Một tin nhắn thông thường có thể chứa nhiều câu văn, dấu câu và kéo dài qua nhiều dòng. Bằng cách gửi liên tục payload lớn như thế này, chúng ta có thể đo đạc chính xác hơn lượng Gas tiêu thụ cũng như độ trễ của mạng lưới khi phải xử lý và lưu trữ dữ liệu thực sự vào Block. Chúc bạn test vui vẻ!"
			var text string
			if *pingFlag {
				text = "PING " + longText
			} else {
				text = longText
			}
			startTime := time.Now()
			clientTimestamp := big.NewInt(startTime.UnixMilli())

			payloadData, err := contractAbi.Pack("sendMessage", targetAddress, text, clientTimestamp)
			if err != nil {
				fmt.Printf("❌ Lỗi pack tx %d: %v\n", i+1, err)
				continue
			}

			tx := types.NewTransaction(nonce, contractAddress, big.NewInt(0), 5000000, gasPrice, payloadData)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
			if err != nil {
				fmt.Printf("❌ Lỗi ký tx %d: %v\n", i+1, err)
				continue
			}

			err = client.SendTransaction(context.Background(), signedTx)
			if err != nil {
				fmt.Printf("❌ Lỗi gửi tx %d: %v\n", i+1, err)
			} else {
				if *pingFlag {
					fmt.Printf("   [📤 ĐÃ GỬI PING %d/%d] %s - Đang chờ PONG...\n", i+1, *spamFlag, signedTx.Hash().Hex())
					<-pongCh
					nonce++
				} else {
					fmt.Printf("   [📤 ĐÃ SPAM TX %d/%d] %s - Đang chờ xác nhận...\n", i+1, *spamFlag, signedTx.Hash().Hex())
					nonce++

					// Chờ tx có receipt (đã được đưa vào block) mới gửi tiếp
					for {
						_, errReceipt := client.TransactionReceipt(context.Background(), signedTx.Hash())
						if errReceipt == nil {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
			}
		}
		fmt.Println("✅ Đã spam xong! Giờ bạn có thể chat tay (nếu muốn) hoặc cứ treo để xem log.")
		fmt.Print("> ")
	}

	// 6. Đọc input và gửi
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			fmt.Print("> ")
			continue
		}

		startTime := time.Now()
		clientTimestamp := big.NewInt(startTime.UnixMilli())

		payloadData, err := contractAbi.Pack("sendMessage", targetAddress, text, clientTimestamp)
		if err != nil {
			fmt.Printf("❌ Lỗi pack: %v\n> ", err)
			continue
		}

		nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
		if err != nil {
			fmt.Printf("❌ Lỗi lấy nonce: %v\n> ", err)
			continue
		}

		gasPrice, _ := client.SuggestGasPrice(context.Background())
		if gasPrice == nil {
			gasPrice = big.NewInt(1000000)
		}

		tx := types.NewTransaction(nonce, contractAddress, big.NewInt(0), 5000000, gasPrice, payloadData)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
		if err != nil {
			fmt.Printf("❌ Lỗi ký tx: %v\n> ", err)
			continue
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			fmt.Printf("❌ Lỗi gửi tx: %v\n> ", err)
			continue
		}

		latency := time.Since(startTime)
		fmt.Printf("   [📤 ĐÃ BẮN TX %s] Thời gian đóng gói và gửi: %v\n> ", signedTx.Hash().Hex()[:8], latency)
	}
}
