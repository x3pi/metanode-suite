/*
 * BÀI TEST: 30-eip7702-parallel-contention
 * MÔ TẢ   : Kiểm tra cơ chế Block-STM xử lý xung đột ghi (Write Conflict) đồng thời
 *           trên Storage của một EOA được ủy quyền theo chuẩn EIP-7702 (SetCode).
 * GỌI     : 10 ví gửi giao dịch đồng thời gọi hàm tăng counter vào cùng 1 EOA đã delegate code.
 * KỲ VỌNG : Block-STM phải phát hiện read/write conflict trên storage của Delegated Account,
 *           abort và re-execute tuần tự, đảm bảo giá trị counter cuối cùng bằng đúng 10.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)

type Config struct {
	PrivateKeys []string          `json:"private_keys"`
	RPCUrl      string            `json:"rpc_url"`
	RPCNodes    map[string]string `json:"rpc_nodes"`
	ChainID     int64             `json:"chain_id"`
}

// Bytecode Counter Contract:
// SLOAD(0) + 1 -> SSTORE(0)
// Init code: 600a600c600039600a6000f360005460010160005500
const (
	counterDeployCodeHex = "600a600c600039600a6000f360005460010160005500"
)

func waitForReceipt(client *ethclient.Client, txHash common.Hash) *types.Receipt {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt
		}
		select {
		case <-ctx.Done():
			log.Fatalf("❌ Timeout chờ receipt cho tx %s", txHash.Hex())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 30-eip7702-parallel-contention (XUNG ĐỘT GHI EIP-7702 DƯỚI BLOCK-STM)")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra Block-STM quản lý MVHashMap & Conflict Detection trên EIP-7702:")
	fmt.Println("             1. Deploy Counter logic contract (SLOAD 0 + 1 -> SSTORE 0).")
	fmt.Println("             2. EOA Mục Tiêu (Account 0) ủy quyền EIP-7702 sang Counter Contract.")
	fmt.Println("             3. 10 ví gửi giao dịch ĐỒNG THỜI gọi vào EOA Mục Tiêu.")
	fmt.Println("             4. Tất cả 10 txs đều tranh chấp ghi vào Slot 0 của EOA Mục Tiêu.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phát hiện xung đột trên Delegated EOA Storage, xử lý tuần tự,")
	fmt.Println("             Counter cuối cùng trên EOA Mục Tiêu phải đạt đúng 10.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config %s: %v", configPath, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("❌ Lỗi parse config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	testKeys := loadPrivateKeys("", cfg.PrivateKeys)
	if len(testKeys) < 2 {
		log.Fatalf("❌ Cần ít nhất 2 private key để thực hiện test")
	}

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			log.Fatalf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	signer := types.NewPragueSigner(chainID)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(20000000000)
	}

	pkCaller, _ := crypto.HexToECDSA(testKeys[0])
	addrCaller := crypto.PubkeyToAddress(pkCaller.PublicKey)

	// Sử dụng ví cũ cố định (Account 1 nếu có >= 2 keys, hoặc Account 0)
	var pkEOAB *ecdsa.PrivateKey
	if len(testKeys) > 1 {
		pkEOAB, _ = crypto.HexToECDSA(testKeys[1])
	} else {
		pkEOAB, _ = crypto.HexToECDSA(testKeys[0])
	}
	addrEOAB := crypto.PubkeyToAddress(pkEOAB.PublicKey)

	// -------------------------------------------------------------------------
	// BƯỚC 1: Deploy Contract làm Delegate Target (Logic Counter)
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 1: Deploy Counter Smart Contract làm Delegate Logic...")
	nonceCaller, _ := client.PendingNonceAt(context.Background(), addrCaller)
	deployBytecode, _ := hex.DecodeString(counterDeployCodeHex)

	txDeploy := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonceCaller,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: gasPrice,
		Gas:       300000,
		To:        nil,
		Value:     big.NewInt(0),
		Data:      deployBytecode,
	})
	signedDeploy, _ := types.SignTx(txDeploy, signer, pkCaller)
	if err := client.SendTransaction(context.Background(), signedDeploy); err != nil {
		log.Fatalf("❌ Deploy counter logic contract thất bại: %v", err)
	}
	rcptDeploy := waitForReceipt(client, signedDeploy.Hash())
	if rcptDeploy.Status != 1 {
		log.Fatalf("❌ Deploy counter logic contract reverted!")
	}
	delegateContractAddr := rcptDeploy.ContractAddress
	fmt.Printf("   ✅ Target Counter Contract deployed: %s\n", delegateContractAddr.Hex())

	// -------------------------------------------------------------------------
	// BƯỚC 2: EOA B (Account 1) ký Authorization ủy quyền sang Counter Contract
	// -------------------------------------------------------------------------
	fmt.Printf("\n🔹 BƯỚC 2: EOA B (%s) ký EIP-7702 Authorization ủy quyền...\n", addrEOAB.Hex())
	authNonce, _ := client.PendingNonceAt(context.Background(), addrEOAB)
	authTuple, err := types.SignSetCode(pkEOAB, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContractAddr,
		Nonce:   authNonce,
	})
	if err != nil {
		log.Fatalf("❌ Ký SetCode Authorization thất bại: %v", err)
	}

	// Gửi SetCodeTx từ Account 0 để kích hoạt code trên Account 1
	nonceCaller, _ = client.PendingNonceAt(context.Background(), addrCaller)
	setCodeTx := types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     nonceCaller,
		GasTipCap: uint256.NewInt(1000000000),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       500000,
		To:        addrEOAB,
		Value:     uint256.NewInt(0),
		Data:      nil,
		AuthList:  []types.SetCodeAuthorization{authTuple},
	})
	signedSetCode, err := types.SignTx(setCodeTx, signer, pkCaller)
	if err != nil {
		log.Fatalf("❌ Ký SetCodeTx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedSetCode); err != nil {
		log.Fatalf("❌ Gửi SetCodeTx thất bại: %v", err)
	}
	rcptSetCode := waitForReceipt(client, signedSetCode.Hash())
	if rcptSetCode.Status != 1 {
		log.Fatalf("❌ Kích hoạt EIP-7702 SetCode thất bại!")
	}
	fmt.Printf("   ✅ EOA B %s đã kích hoạt EIP-7702 ủy quyền sang %s thành công!\n", addrEOAB.Hex(), delegateContractAddr.Hex())

	// Kiểm tra code tại EOA B
	codeAfterDelegate, _ := client.CodeAt(context.Background(), addrEOAB, nil)
	expectedDesignator := "ef0100" + delegateContractAddr.Hex()[2:]
	fmt.Printf("   🔍 Bytecode tại EOA B: 0x%x\n", codeAfterDelegate)
	if !strings.EqualFold(hex.EncodeToString(codeAfterDelegate), expectedDesignator) {
		log.Fatalf("   ❌ Bytecode delegation designator không khớp! Got: %s, Want: %s", hex.EncodeToString(codeAfterDelegate), expectedDesignator)
	}

	// -------------------------------------------------------------------------
	// BƯỚC 3: Gửi 10 giao dịch đồng thời gọi vào EOA B (Delegated Account)
	// -------------------------------------------------------------------------
	// Đọc giá trị Slot 0 hiện tại trước khi gửi batch để hỗ trợ cả ví cũ đã từng chạy test
	initialSlot0Bytes, _ := client.StorageAt(context.Background(), addrEOAB, common.Hash{}, nil)
	initialSlot0 := new(big.Int).SetBytes(initialSlot0Bytes)
	fmt.Printf("   🔍 Giá trị Slot 0 ban đầu của EOA B: %s\n", initialSlot0.String())

	numCallers := len(testKeys)
	if numCallers > 10 {
		numCallers = 10
	}
	fmt.Printf("\n🔹 BƯỚC 3: Gửi %d giao dịch ĐỒNG THỜI từ %d ví gọi vào EOA B %s...\n", numCallers, numCallers, addrEOAB.Hex())

	var wg sync.WaitGroup
	txHashes := make([]common.Hash, numCallers)
	startSend := time.Now()

	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func(idx int, pkStr string) {
			defer wg.Done()
			pkC, err := crypto.HexToECDSA(pkStr)
			if err != nil {
				return
			}
			callerA := crypto.PubkeyToAddress(pkC.PublicKey)
			nonce, err := client.PendingNonceAt(context.Background(), callerA)
			if err != nil {
				return
			}

			txCall := types.NewTx(&types.DynamicFeeTx{
				ChainID:   chainID,
				Nonce:     nonce,
				GasTipCap: big.NewInt(1000000000),
				GasFeeCap: gasPrice,
				Gas:       200000,
				To:        &addrEOAB, // GỌI TRỰC TIẾP VÀO ĐỊA CHỈ EOA B
				Value:     big.NewInt(0),
				Data:      common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000001"),
			})
			signedCall, err := types.SignTx(txCall, signer, pkC)
			if err != nil {
				return
			}
			if err := client.SendTransaction(context.Background(), signedCall); err != nil {
				fmt.Printf("   ❌ Wallet %d gửi tx lỗi: %v\n", idx, err)
				return
			}
			txHashes[idx] = signedCall.Hash()
			fmt.Printf("   ✅ Wallet %d gửi tx thành công: %s\n", idx, signedCall.Hash().Hex())
		}(i, testKeys[i])
	}
	wg.Wait()

	fmt.Println("\n⏳ Đang chờ xác nhận toàn bộ giao dịch...")
	successCount := 0
	for i, h := range txHashes {
		if h == (common.Hash{}) {
			continue
		}
		rcpt := waitForReceipt(client, h)
		if rcpt != nil && rcpt.Status == 1 {
			successCount++
			fmt.Printf("   ✅ Tx %d (%s) thành công trong block %d\n", i, h.Hex()[:10]+"...", rcpt.BlockNumber.Uint64())
		}
	}

	// -------------------------------------------------------------------------
	// BƯỚC 4: Kiểm tra giá trị Storage Slot 0 tại địa chỉ EOA Mục Tiêu
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 4: Kiểm tra giá trị Slot 0 trên Storage của EOA Mục Tiêu...")
	slot0Bytes, err := client.StorageAt(context.Background(), addrEOAB, common.Hash{}, nil)
	if err != nil {
		log.Fatalf("❌ Đọc StorageAt thất bại: %v", err)
	}
	valSlot0 := new(big.Int).SetBytes(slot0Bytes)
	expectedSlot0 := new(big.Int).Add(initialSlot0, big.NewInt(int64(successCount)))
	elapsed := time.Since(startSend)

	fmt.Println("\n==========================================================")
	fmt.Println("📊 BÁO CÁO KẾT QUẢ TEST EIP-7702 CONTENTION:")
	fmt.Println("==========================================================")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Tổng số tx gửi     : %d\n", numCallers)
	fmt.Printf("Số tx thành công   : %d\n", successCount)
	fmt.Printf("Giá trị ban đầu    : %s\n", initialSlot0.String())
	fmt.Printf("Giá trị thực tế    : %s\n", valSlot0.String())
	fmt.Printf("Giá trị kỳ vọng    : %s (+%d)\n", expectedSlot0.String(), successCount)

	if valSlot0.Cmp(expectedSlot0) == 0 && successCount == numCallers {
		fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý hoàn hảo xung đột Read/Write trên EIP-7702 Delegated Storage!")
	} else {
		fmt.Printf("\n❌ TEST FAILED: Sai lệch giá trị! Kỳ vọng %s, Thực tế %s\n", expectedSlot0.String(), valSlot0.String())
		os.Exit(1)
	}
}

// loadPrivateKeys loads private keys either from an explicitly passed keys file,
// or defaults to the keys defined in config.json.
func loadPrivateKeys(keysFilePath string, cfgKeys []string) []string {
	if keysFilePath != "" {
		raw, err := os.ReadFile(keysFilePath)
		if err != nil {
			log.Fatalf("❌ Lỗi đọc file keys %s: %v", keysFilePath, err)
		}
		var strKeys []string
		if err := json.Unmarshal(raw, &strKeys); err == nil && len(strKeys) > 0 {
			return strKeys
		}
		var genKeys []struct {
			PrivateKey string `json:"private_key"`
		}
		if err := json.Unmarshal(raw, &genKeys); err == nil && len(genKeys) > 0 {
			var res []string
			for _, gk := range genKeys {
				if gk.PrivateKey != "" {
					res = append(res, gk.PrivateKey)
				}
			}
			if len(res) > 0 {
				return res
			}
		}
		log.Fatalf("❌ Không thể parse private key nào từ file %s", keysFilePath)
	}
	if len(cfgKeys) > 0 {
		return cfgKeys
	}
	log.Fatalf("❌ Không tìm thấy private key nào trong config.json hoặc file chỉ định")
	return nil
}
