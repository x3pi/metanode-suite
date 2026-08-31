/*
 * BÀI TEST: 26-eip7702-setcode-tx
 * MÔ TẢ   : Kiểm tra loại giao dịch EIP-7702 SetCode Transaction (TxType = 0x04).
 * GỌI     : Ký Authorization tuple cho EOA bằng SignSetCode, gói vào SetCodeTx, ký bằng PragueSigner và gửi qua RPC.
 * KỲ VỌNG : Giao dịch được Mempool tiếp nhận, xử lý thành công trong block, receipt trả về Type = 0x04 và status = 1.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-blockstm/config"
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)


func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 26-eip7702-setcode-tx")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra loại giao dịch EIP-7702 SetCode Transaction (TxType = 0x04).")
	fmt.Println("⚡ GỌI     : Ký Authorization tuple bằng SignSetCode, gói vào SetCodeTx, ký bằng PragueSigner và gửi qua RPC.")
	fmt.Println("🎯 KỲ VỌNG : Giao dịch được Mempool tiếp nhận, xử lý thành công trong block, receipt trả về Type = 0x04 và status = 1.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Cần ít nhất 1 private key để test")
	}

	pkSender, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		log.Fatalf("❌ Parse private key sender thất bại: %v", err)
	}
	senderAddr := crypto.PubkeyToAddress(*pkSender.Public().(*ecdsa.PublicKey))

	// Chuẩn bị Authority Account (EOA ủy quyền)
	var pkAuthority *ecdsa.PrivateKey
	if len(cfg.PrivateKeys) > 1 {
		pkAuthority, err = crypto.HexToECDSA(cfg.PrivateKeys[1])
		if err != nil {
			log.Fatalf("❌ Parse private key authority thất bại: %v", err)
		}
	} else {
		// Dùng chính sender hoặc sinh ví mới
		pkAuthority = pkSender
	}
	authorityAddr := crypto.PubkeyToAddress(*pkAuthority.Public().(*ecdsa.PublicKey))

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			log.Fatalf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	senderNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		log.Fatalf("❌ Lấy nonce sender thất bại: %v", err)
	}

	authNonce, err := client.PendingNonceAt(context.Background(), authorityAddr)
	if err != nil {
		log.Fatalf("❌ Lấy nonce authority thất bại: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(20000000000) // 20 gwei
	}
	gasTipCap, err := client.SuggestGasTipCap(context.Background())
	if err != nil {
		gasTipCap = big.NewInt(1000000000) // 1 gwei
	}

	// Smart contract delegate mục tiêu (ví dụ một contract wallet logic)
	delegateContract := common.HexToAddress("0x0000000000000000000000000000000000007702")

	fmt.Printf("🔑 Sender (Relayer / Caller): %s (Nonce: %d)\n", senderAddr.Hex(), senderNonce)
	fmt.Printf("🛡️ Authority (EOA Delegate): %s (Nonce: %d)\n", authorityAddr.Hex(), authNonce)
	fmt.Printf("📋 Delegate Code Contract: %s\n", delegateContract.Hex())
	fmt.Printf("🌐 ChainID: %s\n", chainID.String())

	// 1. Tạo và Ký Authorization Tuple (EIP-7702)
	authTuple := types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContract,
		Nonce:   authNonce,
	}

	signedAuth, err := types.SignSetCode(pkAuthority, authTuple)
	if err != nil {
		log.Fatalf("❌ Ký SetCode Authorization thất bại: %v", err)
	}
	fmt.Printf("✍️ Đã ký EIP-7702 Authorization cho %s -> delegate %s (Nonce: %d)\n", authorityAddr.Hex(), delegateContract.Hex(), authNonce)

	// 2. Cấu hình SetCodeTx
	// Mục tiêu (To) có thể là địa chỉ authorityAddr hoặc contract tương tác
	toAddr := authorityAddr
	setCodeTxData := &types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     senderNonce,
		GasTipCap: uint256.MustFromBig(gasTipCap),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       250000,
		To:        toAddr,
		Value:     uint256.NewInt(0),
		Data:      []byte{},
		AuthList:  []types.SetCodeAuthorization{signedAuth},
	}

	// 3. Ký giao dịch với Prague Signer (EIP-7702 yêu cầu PragueSigner)
	signer := types.NewPragueSigner(chainID)
	signedTx, err := types.SignNewTx(pkSender, signer, setCodeTxData)
	if err != nil {
		log.Fatalf("❌ Ký SetCodeTx thất bại: %v", err)
	}

	fmt.Printf("📤 Gửi SetCodeTx hash: %s (TxType: 0x%02x)...\n", signedTx.Hash().Hex(), signedTx.Type())

	// 4. Gửi qua RPC
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("❌ Lỗi gửi SetCodeTx qua RPC: %v", err)
	}
	fmt.Printf("✅ Đã gửi thành công lên RPC! Đang chờ receipt...\n")

	// 5. Chờ Transaction Receipt
	var receipt *types.Receipt
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err = client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if receipt == nil {
		log.Fatalf("❌ Timeout chờ receipt cho tx %s", signedTx.Hash().Hex())
	}

	fmt.Printf("\n📄 RECEIPT SUMMARY:\n")
	fmt.Printf("   - Block Number: %d\n", receipt.BlockNumber.Uint64())
	fmt.Printf("   - Status: %d (1 = Success, 0 = Revert)\n", receipt.Status)
	fmt.Printf("   - Tx Type: 0x%02x (Kỳ vọng: 0x04)\n", receipt.Type)
	fmt.Printf("   - Gas Used: %d\n", receipt.GasUsed)

	// 6. Kiểm tra code của Authority Account sau khi áp dụng EIP-7702
	code, err := client.CodeAt(context.Background(), authorityAddr, nil)
	if err == nil {
		fmt.Printf("   - Authority Account Code: 0x%x (len: %d)\n", code, len(code))
	}

	if receipt.Status != 1 {
		fmt.Println("❌ TEST FAILED: Transaction Status != 1 (Reverted)")
		os.Exit(1)
	}

	if receipt.Type != types.SetCodeTxType {
		fmt.Printf("❌ TEST FAILED: TxType không khớp (nhận được 0x%02x, kỳ vọng 0x%02x)\n", receipt.Type, types.SetCodeTxType)
		os.Exit(1)
	}

	fmt.Println("\n🎉 TEST 26 (EIP-7702 SETCODE TX) PASSED THÀNH CÔNG!")
}
