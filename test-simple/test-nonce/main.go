package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	client, err := ethclient.Dial("http://127.0.0.1:8545")
	if err != nil {
		log.Fatalf("❌ Không thể kết nối tới RPC: %v", err)
	}

	privateKeyHex := "fb64857fe95b55dff91a11d2da0c8db2dddb29f617d3d1ddaa9a9880733d5407"
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatalf("❌ Lỗi load private key: %v", err)
	}
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatalf("❌ Lỗi chuyển đổi public key")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Printf("📬 Sử dụng địa chỉ: %s\n", fromAddress.Hex())

	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy nonce: %v", err)
	}
	fmt.Printf("🔢 Nonce hiện tại: %d\n", nonce)

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		log.Fatalf("❌ Lỗi lấy ChainID: %v", err)
	}

	value := big.NewInt(1000000000000000)
	gasLimit := uint64(21000)
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		log.Fatalf("❌ Lỗi lấy gas price: %v", err)
	}

	toAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")

	// TEST 1: Gửi nonce = 0 (Nonce quá thấp)
	fmt.Printf("\n🚀 TEST 1: Đang thử gửi TX với NONCE = 0 (quá thấp)...\n")
	tx0 := types.NewTransaction(0, toAddress, value, gasLimit, gasPrice, nil)
	signedTx0, err := types.SignTx(tx0, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatalf("❌ Lỗi ký TX 0: %v", err)
	}

	err = client.SendTransaction(context.Background(), signedTx0)
	if err != nil {
		fmt.Printf("✅ TEST PASS: Node đã trả về lỗi từ chối nonce 0!\nChi tiết lỗi: %v\n", err)
	} else {
		fmt.Printf("❌ TEST FAIL: Node KHÔNG trả về lỗi cho giao dịch nonce 0!\n")
	}

	time.Sleep(1 * time.Second)

	// TEST 2: Gửi data siêu to khổng lồ (>6MB) để test Invalid Data (hoặc cấu trúc hỏng)
	fmt.Printf("\n🚀 TEST 2: Đang thử gửi TX với DATA RẤT LỚN (Invalid Data)...\n")
	hugeData := make([]byte, 7*1024*1024) // 7MB data (Vượt qua mức 6MB giới hạn trong validation.go)
	txData := types.NewTransaction(nonce, toAddress, value, gasLimit, gasPrice, hugeData)
	signedTxData, err := types.SignTx(txData, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatalf("❌ Lỗi ký TX Data: %v", err)
	}

	err = client.SendTransaction(context.Background(), signedTxData)
	if err != nil {
		fmt.Printf("✅ TEST PASS: Node đã trả về lỗi từ chối data quá lớn!\nChi tiết lỗi: %v\n", err)
	} else {
		fmt.Printf("❌ TEST FAIL: Node KHÔNG trả về lỗi cho giao dịch data sai!\n")
	}
}
