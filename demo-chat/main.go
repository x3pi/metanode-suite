//go:build ignore

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	tx_helper "tool-test/pkg/client-tcp/utils/tx_helper"
	pb "tool-test/pkg/proto"
)

var (
	configFlag   = flag.String("config", "user1.json", "Đường dẫn đến file cấu hình TCP")
	contractFlag = flag.String("contract", "", "Địa chỉ của contract SimpleChat (nếu để trống và có flag -deploy, sẽ tự deploy)")
	deployFlag   = flag.Bool("deploy", false, "Chỉ định deploy contract mới")
	targetFlag   = flag.String("target", "", "Địa chỉ người nhận tin nhắn (nếu trống, sẽ tự nhận diện người thứ 2 dựa vào config)")
	spamFlag     = flag.Int("spam", 0, "Số lượng tin nhắn cần spam tự động")
	pingFlag     = flag.Bool("ping", false, "Kích hoạt chế độ đo Ping-Pong (gửi PING, chờ PONG, đo RTT)")
)

func main() {
	flag.Parse()

	// 1. Đọc config
	cfgRaw, err := tcp_config.LoadConfig(*configFlag)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file config: %v", err)
	}
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	fmt.Println("==================================================")
	fmt.Printf("🚀 START TCP CHAT CLI (-config=%s)\n", *configFlag)
	fmt.Println("==================================================")

	fromAddress := common.HexToAddress(cfg.ParentAddress)
	fmt.Printf("👤 Địa chỉ của bạn: %s\n", fromAddress.Hex())

	// 2. Load ABI và Bytecode
	abiFile, err := os.Open("SimpleChat.abi")
	if err != nil {
		log.Fatalf("❌ Lỗi mở file SimpleChat.abi: %v", err)
	}
	contractAbi, err := abi.JSON(abiFile)
	abiFile.Close()
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecodeBytes, err := os.ReadFile("SimpleChat.bin")
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file SimpleChat.bin: %v", err)
	}
	bytecodeHex := string(bytecodeBytes)
	if !strings.HasPrefix(bytecodeHex, "0x") {
		bytecodeHex = "0x" + bytecodeHex
	}
	bytecode, err := hexutil.Decode(bytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode: %v", err)
	}

	// 3. Kết nối TCP
	fmt.Printf("🔄 Đang kết nối tới %s...\n", cfg.ConnectionAddress())
	cli, err := client_tcp.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối TCP: %v", err)
	}
	time.Sleep(1 * time.Second)
	fmt.Println("✅ Đã kết nối TCP thành công!")

	var contractAddress common.Address

	// 4. Deploy nếu yêu cầu
	if *deployFlag && *contractFlag == "" {
		fmt.Println("▶️ Đang Deploy contract SimpleChat...")
		emptyAddress := common.Address{}
		receipt, err := tx_helper.SendTransaction(
			"deploy", cli, cfg, emptyAddress, fromAddress, bytecode,
			&tx_models.TxOptions{MaxGas: 5000000, MaxGasPrice: 2000000000},
		)
		if err != nil {
			log.Fatalf("❌ Lỗi deploy: %v", err)
		}
		if receipt != nil && (receipt.Status() == pb.RECEIPT_STATUS_RETURNED || receipt.Status() == pb.RECEIPT_STATUS_HALTED) {
			addr := receipt.ToAddress()
			if addr == (common.Address{}) {
				retBytes := receipt.Return()
				if len(retBytes) >= 20 {
					addr = common.BytesToAddress(retBytes)
				}
			}
			fmt.Printf("🎉 DEPLOY THÀNH CÔNG! Address: %s\n", addr.Hex())
			contractAddress = addr
		} else {
			log.Fatalf("❌ DEPLOY THẤT BẠI! Status: %v", receipt.Status())
		}
	} else if *contractFlag != "" {
		contractAddress = common.HexToAddress(*contractFlag)
	} else {
		log.Fatalf("❌ Cần cung cấp -contract <address> hoặc thêm cờ -deploy")
	}

	fmt.Printf("📝 Đang sử dụng Contract Chat tại: %s\n", contractAddress.Hex())

	// Xác định địa chỉ người nhận nếu chưa truyền
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
	fmt.Println("==================================================")
	fmt.Println("💬 CHAT ĐÃ SẴN SÀNG! Bạn có thể gõ nội dung và nhấn Enter.")
	fmt.Println("==================================================")

	// 5. Lắng nghe Event
	var totalLatencyMs int64
	var msgCount int64
	pongCh := make(chan struct{}, 1)

	eventCh, err := cli.ParentSubcribes([]common.Address{contractAddress})
	if err != nil {
		log.Fatalf("❌ Lỗi subscribe: %v", err)
	}

	go func() {
		for evt := range eventCh {
			logs := evt.EventLogList()
			for _, logItem := range logs {
				topics := logItem.Topics()
				if len(topics) == 0 {
					continue
				}
				eventSigStr := topics[0]
				if !strings.HasPrefix(eventSigStr, "0x") {
					eventSigStr = "0x" + eventSigStr
				}

				// Tìm MessageSent event
				for name, event := range contractAbi.Events {
					if event.ID.Hex() != eventSigStr || name != "MessageSent" {
						continue
					}
					decoded := make(map[string]interface{})
					dataBytes := common.FromHex(logItem.Data())
					if err := contractAbi.UnpackIntoMap(decoded, name, dataBytes); err != nil {
						fmt.Printf("⚠️ Lỗi decode event: %v\n", err)
						continue
					}

					// Lấy indexed params
					topicIdx := 1
					var from, to common.Address
					for _, input := range event.Inputs {
						if input.Indexed && topicIdx < len(topics) {
							hash := common.HexToHash(topics[topicIdx])
							if input.Name == "from" {
								from = common.BytesToAddress(hash.Bytes())
							} else if input.Name == "to" {
								to = common.BytesToAddress(hash.Bytes())
							}
							topicIdx++
						}
					}

					msgText := decoded["message"].(string)
					msgTimestamp := decoded["timestamp"].(*big.Int)

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
								payloadData, _ := contractAbi.Pack("sendMessage", target, pText, ts)
								tx_helper.SendTransaction(
									"sendMessage", cli, cfg, contractAddress, fromAddress, payloadData,
									&tx_models.TxOptions{MaxGas: 5000000},
								)
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
	}()

	if *spamFlag > 0 {
		if *pingFlag {
			fmt.Printf("🚀 Đang tự động chơi Ping-Pong %d rounds qua TCP...\n", *spamFlag)
		} else {
			fmt.Printf("🚀 Đang tự động spam %d tin nhắn qua TCP...\n", *spamFlag)
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
				fmt.Printf("❌ Lỗi pack dữ liệu: %v\n", err)
				continue
			}

			// Gửi giao dịch qua TCP (tx_helper.SendTransaction mặc định đã block để đợi receipt)
			receipt, err := tx_helper.SendTransaction(
				"sendMessage", cli, cfg, contractAddress, fromAddress, payloadData,
				&tx_models.TxOptions{MaxGas: 5000000},
			)

			if err != nil {
				fmt.Printf("❌ Lỗi gửi tin %d: %v\n", i+1, err)
			} else if receipt != nil && (receipt.Status() == pb.RECEIPT_STATUS_RETURNED || receipt.Status() == pb.RECEIPT_STATUS_HALTED) {
				if *pingFlag {
					fmt.Printf("   [📤 ĐÃ GỬI PING %d/%d] %s - Đang chờ PONG...\n", i+1, *spamFlag, receipt.TransactionHash().Hex())
					<-pongCh
				} else {
					fmt.Printf("   [📤 ĐÃ SPAM TX %d/%d] %s\n", i+1, *spamFlag, receipt.TransactionHash().Hex())
				}
			} else {
				fmt.Printf("❌ Tin nhắn %d gửi thất bại\n", i+1)
			}
		}
		fmt.Println("✅ Đã spam xong! Giờ bạn có thể chat tay (nếu muốn) hoặc cứ treo để xem log.")
		fmt.Print("> ")
	}

	// 6. Đọc input và gửi tin nhắn
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			fmt.Print("> ")
			continue
		}

		// Tính thời gian gửi
		startTime := time.Now()
		clientTimestamp := big.NewInt(startTime.UnixMilli())

		// Đóng gói data: function sendMessage(address _to, string calldata _message, uint256 _clientTimestamp)
		payloadData, err := contractAbi.Pack("sendMessage", targetAddress, text, clientTimestamp)
		if err != nil {
			fmt.Printf("❌ Lỗi pack dữ liệu: %v\n> ", err)
			continue
		}

		// Gửi giao dịch
		receipt, err := tx_helper.SendTransaction(
			"sendMessage", cli, cfg, contractAddress, fromAddress, payloadData,
			&tx_models.TxOptions{MaxGas: 5000000},
		)

		latency := time.Since(startTime)

		if err != nil {
			fmt.Printf("❌ Lỗi gửi tin: %v\n> ", err)
		} else if receipt != nil && (receipt.Status() == pb.RECEIPT_STATUS_RETURNED || receipt.Status() == pb.RECEIPT_STATUS_HALTED) {
			fmt.Printf("   [📤 GỬI THÀNH CÔNG] Độ trễ (Send latency): %v\n> ", latency)
		} else {
			statusStr := "UNKNOWN"
			if receipt != nil {
				statusStr = receipt.Status().String()
			}
			fmt.Printf("❌ Tin nhắn gửi thất bại (Status: %s)\n> ", statusStr)
		}
	}
}
