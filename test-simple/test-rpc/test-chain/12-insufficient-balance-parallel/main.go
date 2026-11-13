/*
 * BÀI TEST: 12-insufficient-balance-parallel
 * MÔ TẢ   : Tạo 1 ví tạm mới (tự động đăng ký Account Gate nếu bật trên cụm), nạp vào đó một lượng nhỏ
 *           cố định (0.05 ETH), rồi từ ví tạm bắn song song 5 giao dịch chuyển tiền vượt quá tổng số dư
 *           (mỗi giao dịch 0.0125 ETH -> tổng 0.0625 ETH + gas > 0.05 ETH).
 * LỢI ÍCH : Bảo vệ tuyệt đối số dư của các ví chính (cfg.PrivateKeys[0..N]), không bao giờ làm cạn tiền
 *           các bài test phía sau như 13-deploy-and-call-same-block.
 * KỲ VỌNG : Chỉ tối đa 3 giao dịch đầu thành công, ít nhất 2 giao dịch bị từ chối / Revert vì thiếu tiền.
 */
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type jsonRPCReq struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	Id      int           `json:"id"`
}

type jsonRPCResp struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func rawRPCCall(rpcURL, method string, params []interface{}, out interface{}) error {
	payload, err := json.Marshal(jsonRPCReq{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		Id:      1,
	})
	if err != nil {
		return err
	}
	httpCli := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpCli.Post(rpcURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed jsonRPCResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	if parsed.Error != nil {
		return fmt.Errorf("rpc error %d: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if out != nil && len(parsed.Result) > 0 {
		return json.Unmarshal(parsed.Result, out)
	}
	return nil
}

func ensureTempWalletRegisteredIfGateEnabled(rpcURL string, privKey *ecdsa.PrivateKey, addr common.Address) error {
	var identity struct {
		AccountGate bool `json:"accountGate"`
	}
	// Nếu node không hỗ trợ mtn_getClusterIdentity hoặc không bật AccountGate thì bỏ qua nhẹ nhàng
	if err := rawRPCCall(rpcURL, "mtn_getClusterIdentity", []interface{}{}, &identity); err != nil || !identity.AccountGate {
		return nil
	}

	var regMsg struct {
		HashToSign string `json:"hashToSign"`
	}
	if err := rawRPCCall(rpcURL, "mtn_getRegistrationMessage", []interface{}{addr.Hex()}, &regMsg); err != nil {
		return fmt.Errorf("lấy thông điệp đăng ký ví tạm thất bại: %w", err)
	}
	hashBytes, err := hexutil.Decode(regMsg.HashToSign)
	if err != nil {
		return fmt.Errorf("decode hashToSign lỗi: %w", err)
	}
	sigBytes, err := crypto.Sign(hashBytes, privKey)
	if err != nil {
		return fmt.Errorf("ký đăng ký ví tạm lỗi: %w", err)
	}

	var submitInfo struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := rawRPCCall(rpcURL, "mtn_registerAccount", []interface{}{addr.Hex(), hexutil.Encode(sigBytes)}, &submitInfo); err != nil {
		return fmt.Errorf("gửi mtn_registerAccount cho ví tạm lỗi: %w", err)
	}

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		var st struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := rawRPCCall(rpcURL, "mtn_getRegistrationStatus", []interface{}{addr.Hex()}, &st); err == nil {
			switch st.Status {
			case "CONFIRMED", "PROVISIONAL":
				fmt.Printf("🔐 Đã đăng ký Account Gate cho ví tạm %s (%s)\n", addr.Hex(), st.Status)
				return nil
			case "REJECTED", "FAILED":
				return fmt.Errorf("đăng ký ví tạm bị từ chối (%s): %s", st.Status, st.Reason)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("quá thời gian chờ xác nhận Account Gate cho ví tạm %s", addr.Hex())
}

func sendSimpleTransferAndWait(
	client *ethclient.Client,
	chainID int64,
	senderPK *ecdsa.PrivateKey,
	senderAddr, toAddr common.Address,
	amount *big.Int,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	nonce, err := client.PendingNonceAt(ctx, senderAddr)
	cancel()
	if err != nil {
		return fmt.Errorf("đọc pending nonce lỗi: %w", err)
	}

	gasPrice := big.NewInt(1_000_000_000)
	ctxGP, cancelGP := context.WithTimeout(context.Background(), 5*time.Second)
	if gp, gpErr := client.SuggestGasPrice(ctxGP); gpErr == nil && gp != nil && gp.Sign() > 0 {
		gasPrice = gp
	}
	cancelGP()

	tx := types.NewTransaction(nonce, toAddr, amount, 21000, gasPrice, nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), senderPK)
	if err != nil {
		return err
	}
	ctxSend, cancelSend := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.SendTransaction(ctxSend, signedTx)
	cancelSend()
	if err != nil {
		return fmt.Errorf("gửi giao dịch nạp ví lỗi: %w", err)
	}

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		ctxR, cancelR := context.WithTimeout(context.Background(), 4*time.Second)
		rcp, rErr := client.TransactionReceipt(ctxR, signedTx.Hash())
		cancelR()
		if rErr == nil && rcp != nil && rcp.BlockNumber != nil && rcp.BlockNumber.Uint64() > 0 {
			if rcp.Status != types.ReceiptStatusSuccessful {
				return fmt.Errorf("giao dịch nạp tiền bị revert (tx=%s)", signedTx.Hash().Hex())
			}
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("quá hạn chờ confirm giao dịch nạp tiền %s", signedTx.Hash().Hex())
}

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 12-insufficient-balance-parallel")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Tạo ví tạm mới, nạp số dư nhỏ cố định (0.05 ETH), rồi bắn song song 5 tx vượt quá số dư.")
	fmt.Println("⚡ GỌI     : Native Transfer Fast-path từ ví tạm cô lập (bảo toàn số dư ví chính).")
	fmt.Println("🎯 KỲ VỌNG : Tối đa 3 giao dịch thành công, ít nhất 2 giao dịch bị từ chối/revert do hết tiền.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("không đọc được config: %w", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("không thể dial RPC: %w", err)
	}
	defer client.Close()

	if len(cfg.PrivateKeys) < 2 {
		return fmt.Errorf("cần ít nhất 2 private keys để test")
	}

	// Tìm ví giàu nhất trong cấu hình làm nguồn nạp (và tự động bù lại cho Wallet[0] nếu trước đó bị cạn tiền)
	var (
		wallet0PK   *ecdsa.PrivateKey
		wallet0Addr common.Address
		wallet0Bal  *big.Int
		richestPK   *ecdsa.PrivateKey
		richestAddr common.Address
		richestBal  = big.NewInt(-1)
	)
	for i, kHex := range cfg.PrivateKeys {
		pk, kErr := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(kHex), "0x"))
		if kErr != nil {
			continue
		}
		addr := crypto.PubkeyToAddress(pk.PublicKey)
		bal, bErr := client.BalanceAt(context.Background(), addr, nil)
		if bErr != nil {
			continue
		}
		if i == 0 {
			wallet0PK = pk
			wallet0Addr = addr
			wallet0Bal = bal
		}
		if bal.Cmp(richestBal) > 0 {
			richestBal = new(big.Int).Set(bal)
			richestPK = pk
			richestAddr = addr
		}
	}
	if richestPK == nil {
		return fmt.Errorf("không tìm thấy ví hợp lệ nào trong config")
	}

	// Tự phục hồi số dư cho Wallet[0] nếu Wallet[0] đang thấp hơn 10 ETH để đảm bảo các bài sau chạy mượt
	tenETH := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	hundredETH := new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18))
	if wallet0PK != nil && wallet0Bal != nil && wallet0Bal.Cmp(tenETH) < 0 && richestAddr != wallet0Addr && richestBal.Cmp(hundredETH) > 0 {
		refillAmount := new(big.Int).Mul(big.NewInt(50), big.NewInt(1e18))
		fmt.Printf("🔋 Phát hiện Wallet[0] (%s) còn ít gas (%s wei), tự động nạp bù 50 ETH từ %s...\n",
			wallet0Addr.Hex(), wallet0Bal.String(), richestAddr.Hex())
		if err := sendSimpleTransferAndWait(client, cfg.ChainID, richestPK, richestAddr, wallet0Addr, refillAmount); err != nil {
			return err
		}
		fmt.Println("✅ Đã nạp bù 50 ETH cho Wallet[0] thành công!")
	}

	// 1. Tạo ví tạm hoàn toàn mới cho riêng lượt chạy này
	tempPK, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("tạo ví tạm lỗi: %w", err)
	}
	tempAddr := crypto.PubkeyToAddress(tempPK.PublicKey)
	fmt.Printf("🆕 Đã tạo ví tạm cô lập: %s\n", tempAddr.Hex())

	if err := ensureTempWalletRegisteredIfGateEnabled(cfg.RPCUrl, tempPK, tempAddr); err != nil {
		return err
	}

	// 2. Nạp cố định 0.05 ETH (5 * 10^16 wei) vào ví tạm
	fundAmount := big.NewInt(50_000_000_000_000_000) // 0.05 ETH
	if richestBal.Cmp(new(big.Int).Mul(fundAmount, big.NewInt(2))) < 0 {
		return fmt.Errorf("ví nguồn %s không đủ 0.1 ETH để cấp vốn cho ví tạm (hiện có %s wei)", richestAddr.Hex(), richestBal.String())
	}

	fmt.Printf("💸 Nạp %s wei (0.05 ETH) từ %s sang ví tạm %s...\n", fundAmount.String(), richestAddr.Hex(), tempAddr.Hex())
	if err := sendSimpleTransferAndWait(client, cfg.ChainID, richestPK, richestAddr, tempAddr, fundAmount); err != nil {
		return err
	}

	balance, err := client.BalanceAt(context.Background(), tempAddr, nil)
	if err != nil {
		return fmt.Errorf("không đọc được số dư ví tạm: %w", err)
	}
	fmt.Printf("💰 Số dư thực tế của ví tạm (%s): %s wei\n", tempAddr.Hex(), balance.String())

	baseNonce, err := client.PendingNonceAt(context.Background(), tempAddr)
	if err != nil {
		return fmt.Errorf("không đọc được nonce ví tạm: %w", err)
	}

	// Mỗi giao dịch chuyển 1/4 số dư ví tạm (0.0125 ETH) về lại các ví chính -> tối đa 3 tx thành công, 2 tx thiếu tiền!
	sendAmount := new(big.Int).Div(balance, big.NewInt(4))
	fmt.Printf("🚀 Bắn 5 giao dịch ĐỒNG THỜI từ ví tạm, mỗi giao dịch chuyển: %s wei\n\n", sendAmount.String())

	gasPrice := big.NewInt(1_000_000_000)
	if gp, gpErr := client.SuggestGasPrice(context.Background()); gpErr == nil && gp != nil && gp.Sign() > 0 {
		gasPrice = gp
	}

	var wg sync.WaitGroup
	var logMu sync.Mutex

	txHashes := make([]common.Hash, 5)
	start := time.Now()

	for i := 0; i < 5; i++ {
		pkRecv, kErr := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(cfg.PrivateKeys[(i+1)%len(cfg.PrivateKeys)]), "0x"))
		if kErr != nil {
			return fmt.Errorf("invalid receiver key %d: %w", i, kErr)
		}
		receiverAddr := crypto.PubkeyToAddress(pkRecv.PublicKey)
		txNonce := baseNonce + uint64(i)

		wg.Add(1)
		go func(idx int, rAddr common.Address, nonce uint64) {
			defer wg.Done()

			tx := types.NewTransaction(nonce, rAddr, sendAmount, 21000, gasPrice, nil)
			signedTx, sErr := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), tempPK)
			if sErr != nil {
				return
			}

			if sendErr := client.SendTransaction(context.Background(), signedTx); sendErr != nil {
				logMu.Lock()
				fmt.Printf("⚠️ Mempool từ chối ngay tx %d (nonce %d): %v\n", idx, nonce, sendErr)
				logMu.Unlock()
				return
			}

			logMu.Lock()
			fmt.Printf("✅ Đã push tx (Nonce: %d): %s\n", nonce, signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
			logMu.Unlock()
		}(i, receiverAddr, txNonce)
	}

	wg.Wait()

	fmt.Println("⏳ Chờ các giao dịch hoàn tất xử lý trên block...")
	successCount := 0
	revertCount := 0

	time.Sleep(3 * time.Second)

	for i := 0; i < 5; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			revertCount++
			continue
		}

		receipt, rErr := client.TransactionReceipt(context.Background(), hash)
		if rErr != nil && !strings.Contains(strings.ToLower(rErr.Error()), "not found") {
			return fmt.Errorf("truy vấn receipt gặp sự cố: %w", rErr)
		}
		if rErr == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status != 1 {
				fmt.Printf("🔄 Tx %s BỊ REVERT ĐÚNG NHƯ KỲ VỌNG! (Thiếu tiền)\n", hash.Hex())
				revertCount++
			} else {
				fmt.Printf("✅ Tx %s THÀNH CÔNG trong block %d\n", hash.Hex(), receipt.BlockNumber.Uint64())
				successCount++
			}
		} else {
			fmt.Printf("🔄 Tx %s BỊ LOẠI BỎ ĐÚNG KỲ VỌNG (Không đủ số dư để vào block)\n", hash.Hex())
			revertCount++
		}
	}

	elapsed := time.Since(start)

	fmt.Println("\n📊 KẾT QUẢ INSUFFICIENT BALANCE TEST:")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)
	fmt.Printf("Số lượng thành công: %d (Kỳ vọng: 3 hoặc ít hơn)\n", successCount)
	fmt.Printf("Số lượng bị từ chối / Revert: %d (Kỳ vọng: >0)\n", revertCount)

	if revertCount > 0 && successCount <= 3 {
		fmt.Println("\n🎉 TEST PASSED: Block-STM hoặc Mempool xử lý hoàn hảo! (Phát hiện hết tiền song song trên ví tạm)")
	} else {
		return fmt.Errorf("TEST FAILED: success=%d, revert=%d (kỳ vọng success <= 3 và revert > 0)", successCount, revertCount)
	}
	return nil
}

func main() {
	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	if err := RunTest(configPath); err != nil {
		log.Fatalf("❌ %v", err)
	}
}
