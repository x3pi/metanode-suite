package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type NodeClient struct {
	Name   string
	URL    string
	Client *ethclient.Client
	Online bool
}

func dialClient(url string) (*ethclient.Client, error) {
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	rpcClient, err := rpc.DialHTTPWithClient(url, httpClient)
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(rpcClient), nil
}

func matchNodeName(name string, filter string) bool {
	if filter == "" {
		return false
	}
	cleanName := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, "m"), "node"))
	cleanFilter := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(filter, "m"), "node"))
	return cleanName == cleanFilter || strings.EqualFold(name, filter)
}

func main() {
	configPath := flag.String("config", "../config.json", "Đường dẫn file config.json")
	txCount := flag.Int("count", 15, "Số lượng giao dịch cần gửi và xác nhận")
	checkFork := flag.Bool("check-fork", true, "Kiểm tra Block Hash & StateRoot giữa các node để đảm bảo không fork")
	requireAllAlive := flag.Bool("require-all-alive", true, "Bắt buộc toàn bộ các node phải đang sống sau đợt test")
	stoppedNode := flag.String("stopped-node", "", "Tên hoặc ID của node đang cố ý bị dừng (ví dụ: '0', 'm0'). Node này được phép offline, các node còn lại bắt buộc phải sống")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private keys trong config")
	}

	// 1. Khởi tạo danh sách các node RPC và kiểm tra kết nối
	var nodes []*NodeClient
	for name, url := range cfg.RPCNodes {
		c, err := dialClient(url)
		online := false
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, bErr := c.BlockNumber(ctx)
			cancel()
			if bErr == nil {
				online = true
			}
		}
		nodes = append(nodes, &NodeClient{
			Name:   name,
			URL:    url,
			Client: c,
			Online: online,
		})
	}

	var activeNodes []*NodeClient
	for _, n := range nodes {
		if n.Online {
			if *stoppedNode != "" && matchNodeName(n.Name, *stoppedNode) {
				continue
			}
			activeNodes = append(activeNodes, n)
		}
	}

	fmt.Println("==========================================================")
	fmt.Printf("🚀 METANODE RESTART & RECOVERY TEST\n")
	fmt.Printf("   • Tổng số node cấu hình: %d\n", len(nodes))
	if *stoppedNode != "" {
		fmt.Printf("   • Node dự kiến TẮT     : %s (ngoại lệ kiểm tra sống/chết)\n", *stoppedNode)
	}
	fmt.Printf("   • Số node đang ONLINE  : %d (", len(activeNodes))
	for i, n := range activeNodes {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s: %s", n.Name, n.URL)
	}
	fmt.Printf(")\n")
	fmt.Printf("   • Số TXs mục tiêu      : %d\n", *txCount)
	fmt.Println("==========================================================")

	if len(activeNodes) == 0 {
		log.Fatalf("❌ Không có node nào online để gửi giao dịch!")
	}

	// 2. Chuẩn bị tài khoản gửi tiền và lấy nonce ban đầu an toàn
	type AccountState struct {
		Key     *ecdsa.PrivateKey
		Address common.Address
		Nonce   uint64
		Mu      sync.Mutex
	}

	var accounts []*AccountState
	for _, kStr := range cfg.PrivateKeys {
		pk, err := crypto.HexToECDSA(strings.TrimPrefix(kStr, "0x"))
		if err == nil {
			addr := crypto.PubkeyToAddress(pk.PublicKey)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			n, nErr := activeNodes[0].Client.PendingNonceAt(ctx, addr)
			cancel()
			if nErr == nil {
				accounts = append(accounts, &AccountState{
					Key:     pk,
					Address: addr,
					Nonce:   n,
				})
			}
		}
	}
	if len(accounts) == 0 {
		log.Fatalf("❌ Không thể khởi tạo tài khoản hợp lệ nào với nonce")
	}

	// 3. Gửi giao dịch có kiểm soát và xác nhận rõ ràng
	fmt.Printf("⏳ Bắt đầu gửi %d giao dịch phân phối đều qua %d node đang sống (sử dụng %d ví)...\n",
		*txCount, len(activeNodes), len(accounts))
	receiverAddr := common.HexToAddress("0x0000000000000000000000000000000000000088")
	chainID := big.NewInt(cfg.ChainID)

	type TxRecord struct {
		Index    int
		NodeName string
		TxHash   common.Hash
		Client   *ethclient.Client
		BlockNum uint64
		Status   bool
	}

	var records []*TxRecord
	var recordsMu sync.Mutex
	var wg sync.WaitGroup

	// Giới hạn concurrency để không nghẽn RPC
	sem := make(chan struct{}, 5)

	for i := 0; i < *txCount; i++ {
		wg.Add(1)
		go func(txIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Cấp phát nonce an toàn, độc quyền theo từng tài khoản
			acct := accounts[txIdx%len(accounts)]
			acct.Mu.Lock()
			nonce := acct.Nonce
			acct.Nonce++
			acct.Mu.Unlock()

			// Chọn node theo round-robin
			targetNode := activeNodes[txIdx%len(activeNodes)]

			// Amount phân biệt để mỗi tx hash luôn là duy nhất
			amount := big.NewInt(int64(1000 + txIdx))
			gasLimit := uint64(21000)
			gasPrice := big.NewInt(1000000000) // 1 Gwei

			tx := types.NewTransaction(nonce, receiverAddr, amount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), acct.Key)
			if err != nil {
				fmt.Printf("   [TX %d/%d] ⚠️ Lỗi ký tx: %v\n", txIdx+1, *txCount, err)
				return
			}

			ctxSend, cancelSend := context.WithTimeout(context.Background(), 4*time.Second)
			err = targetNode.Client.SendTransaction(ctxSend, signedTx)
			cancelSend()

			if err != nil {
				fmt.Printf("   [TX %d/%d] ⚠️ Lỗi gửi tx qua %s (ví %s, nonce %d): %v\n",
					txIdx+1, *txCount, targetNode.Name, acct.Address.Hex()[:10], nonce, err)
				return
			}

			rec := &TxRecord{
				Index:    txIdx + 1,
				NodeName: targetNode.Name,
				TxHash:   signedTx.Hash(),
				Client:   targetNode.Client,
			}

			recordsMu.Lock()
			records = append(records, rec)
			recordsMu.Unlock()
		}(i)
	}

	wg.Wait()
	fmt.Printf("📤 Đã gửi %d/%d giao dịch thành công vào mempool. Đang chờ xác nhận trong Block...\n", len(records), *txCount)

	// 4. Chờ Receipt xác nhận trong Block
	confirmedCount := 0
	for _, rec := range records {
		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 20*time.Second {
				fmt.Printf("   [TX %d/%d] ⚠️ Timeout chờ receipt cho hash: %s\n", rec.Index, *txCount, rec.TxHash.Hex())

				// 🚨 KIỂM TRA SỨC KHỎE TOÀN BỘ CÁC NODE NGAY LẬP TỨC KHI BỊ TIMEOUT RECEIPT
				fmt.Printf("   🔍 [HEALTH PROBE] Giao dịch %s bị timeout receipt! Đang kiểm tra xem có node nào bị crash không...\n",
					rec.TxHash.Hex()[:14]+"...")
				var crashedNodes []string
				for _, n := range nodes {
					if *stoppedNode != "" && matchNodeName(n.Name, *stoppedNode) {
						fmt.Printf("   • Node %s (%s): ⚪ STOPPED (Dự kiến tắt theo kịch bản test)\n", n.Name, n.URL)
						continue
					}
					c, dErr := dialClient(n.URL)
					online := false
					var bNum uint64
					if dErr == nil {
						cCtx, cCancel := context.WithTimeout(context.Background(), 2*time.Second)
						bNum, dErr = c.BlockNumber(cCtx)
						cCancel()
						if dErr == nil {
							online = true
						}
					}
					if online {
						fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block %d)\n", n.Name, n.URL, bNum)
					} else {
						fmt.Printf("   • Node %s (%s): 🔴 DEAD / CRASH! (Lỗi: %v)\n", n.Name, n.URL, dErr)
						crashedNodes = append(crashedNodes, fmt.Sprintf("%s (%s)", n.Name, n.URL))
					}
				}
				if len(crashedNodes) > 0 {
					log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CRASH / KHÔNG PHẢN HỒI KHI CHỜ RECEIPT: %s! BÀI TEST THẤT BẠI NGAY LẬP TỨC!",
						len(crashedNodes), strings.Join(crashedNodes, ", "))
				}
				break
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			receipt, rErr := rec.Client.TransactionReceipt(ctx, rec.TxHash)
			cancel()

			// Trong Metanode, giao dịch native transfer được xác nhận khi có receipt và BlockNumber > 0
			if rErr == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				rec.BlockNum = receipt.BlockNumber.Uint64()
				rec.Status = true
				confirmedCount++
				fmt.Printf("   ✅ [TX %d/%d] Đã confirm tại Block #%d qua %s (Hash: %s)\n",
					rec.Index, *txCount, rec.BlockNum, rec.NodeName, rec.TxHash.Hex()[:14]+"...")
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	fmt.Printf("\n📊 KẾT QUẢ GIAO DỊCH:\n")
	fmt.Printf("   • Tổng số giao dịch đã gửi: %d\n", len(records))
	fmt.Printf("   • Số giao dịch ĐÃ XÁC NHẬN : %d (%.1f%%)\n", confirmedCount, float64(confirmedCount)/float64(*txCount)*100)

	if confirmedCount == 0 {
		log.Fatalf("❌ Không có giao dịch nào được xác nhận vào block! Bài test thất bại!")
	}

	// 5. KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE (ĐẢM BẢO TẤT CẢ CÒN SỐNG NGOẠI TRỪ NODE CỐ Ý DỪNG)
	fmt.Println("\n📡 [CLUSTER HEALTH CHECK] Kiểm tra trạng thái sống/chết của các node...")
	var deadNodes []string
	var aliveNodes []*NodeClient
	nodeHeights := make(map[string]uint64)

	for _, n := range nodes {
		isStoppedExpected := *stoppedNode != "" && matchNodeName(n.Name, *stoppedNode)

		c, err := dialClient(n.URL)
		online := false
		var bNum uint64
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			bNum, err = c.BlockNumber(ctx)
			cancel()
			if err == nil {
				online = true
				n.Client = c
				n.Online = true
				aliveNodes = append(aliveNodes, n)
				nodeHeights[n.Name] = bNum
			}
		}

		if isStoppedExpected {
			if online {
				fmt.Printf("   • Node %s (%s): ⚠️ VẪN ONLINE (Dự kiến đã tắt)\n", n.Name, n.URL)
			} else {
				fmt.Printf("   • Node %s (%s): ⚪ STOPPED (Đã tắt theo kịch bản test)\n", n.Name, n.URL)
			}
			continue
		}

		if online {
			fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block %d)\n", n.Name, n.URL, bNum)
		} else {
			fmt.Printf("   • Node %s (%s): 🔴 DEAD / MẤT KẾT NỐI!\n", n.Name, n.URL)
			deadNodes = append(deadNodes, fmt.Sprintf("%s (%s)", n.Name, n.URL))
		}
	}

	if len(deadNodes) > 0 {
		if *requireAllAlive {
			log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CHẾT NGOÀI DỰ KIẾN: %s! BÀI TEST THẤT BẠI!", len(deadNodes), strings.Join(deadNodes, ", "))
		} else {
			fmt.Printf("⚠️ Cảnh báo: Có %d node bị chết ngoài dự kiến: %s\n", len(deadNodes), strings.Join(deadNodes, ", "))
		}
	} else {
		if *stoppedNode != "" {
			fmt.Printf("✅ TẤT CẢ %d NODE CÒN LẠI (ngoại trừ node %s) ĐỀU ĐANG SỐNG VÀ ĐỒNG THUẬN KHỎE MẠNH!\n", len(aliveNodes), *stoppedNode)
		} else {
			fmt.Printf("✅ TOÀN BỘ %d NODE ĐỀU ĐANG CÒN SỐNG VÀ ĐỒNG BỘ KHỎE MẠNH!\n", len(nodes))
		}
	}

	// 6. KIỂM TRA ZERO-FORK (SO SÁNH BLOCK HASH & STATE ROOT)
	if *checkFork && len(aliveNodes) >= 2 {
		if *stoppedNode != "" {
			fmt.Printf("\n🔍 [ZERO-FORK VERIFICATION] Kiểm tra đối chiếu Block Hash & StateRoot giữa %d node sống (ngoại trừ node %s đã dừng)...\n",
				len(aliveNodes), *stoppedNode)
		} else {
			fmt.Println("\n🔍 [ZERO-FORK VERIFICATION] Kiểm tra đối chiếu Block Hash & StateRoot giữa các node sống...")
		}

		var minHeight uint64 = 0
		first := true
		for _, h := range nodeHeights {
			if first || h < minHeight {
				minHeight = h
				first = false
			}
		}

		if minHeight > 0 {
			startCompare := uint64(1)
			if minHeight > 5 {
				startCompare = minHeight - 4
			}

			forkDetected := false
			for b := startCompare; b <= minHeight; b++ {
				bBig := big.NewInt(int64(b))
				var referenceHash common.Hash
				var referenceRoot common.Hash
				var referenceNode string

				for _, n := range aliveNodes {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					block, bErr := n.Client.BlockByNumber(ctx, bBig)
					cancel()
					if bErr != nil {
						continue
					}

					h := block.Hash()
					root := block.Root()

					if referenceHash == (common.Hash{}) {
						referenceHash = h
						referenceRoot = root
						referenceNode = n.Name
					} else {
						if h != referenceHash {
							forkDetected = true
							fmt.Printf("🚨 FORK DETECTED TẠI BLOCK %d!\n", b)
							fmt.Printf("   - Node %s: Hash=%s\n", referenceNode, referenceHash.Hex())
							fmt.Printf("   - Node %s: Hash=%s\n", n.Name, h.Hex())
						}
						if root != referenceRoot {
							forkDetected = true
							fmt.Printf("🚨 STATE ROOT MISMATCH TẠI BLOCK %d!\n", b)
							fmt.Printf("   - Node %s: StateRoot=%s\n", referenceNode, referenceRoot.Hex())
							fmt.Printf("   - Node %s: StateRoot=%s\n", n.Name, root.Hex())
						}
					}
				}
			}

			if forkDetected {
				log.Fatalf("❌ PHÁT HIỆN FORK GIỮA CÁC NODE! BÀI TEST THẤT BẠI!")
			} else {
				fmt.Printf("🏆 [100%% ZERO-FORK CONFIRMED] Tất cả %d node đồng nhất hoàn hảo từ Block %d đến %d!\n",
					len(aliveNodes), startCompare, minHeight)
			}
		}
	}
}
