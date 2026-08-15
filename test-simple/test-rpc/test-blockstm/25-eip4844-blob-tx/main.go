/*
 * BÀI TEST: 25-eip4844-blob-tx
 * MÔ TẢ   : Kiểm tra loại giao dịch EIP-4844 Blob Transaction (TxType = 0x03).
 * GỌI     : Tạo KZG Blob Sidecar (Blob, Commitment, Proof, Versioned Hash), ký BlobTx và gửi qua RPC.
 * KỲ VỌNG : Giao dịch được Mempool tiếp nhận, xử lý thành công trong block, receipt trả về Type = 0x03 và status = 1.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 25-eip4844-blob-tx")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra loại giao dịch EIP-4844 Blob Transaction (TxType = 0x03).")
	fmt.Println("⚡ GỌI     : Tạo KZG Blob Sidecar (Blob, Commitment, Proof, Versioned Hash), ký BlobTx và gửi qua RPC.")
	fmt.Println("🎯 KỲ VỌNG : Giao dịch được Mempool tiếp nhận, xử lý thành công trong block, receipt trả về Type = 0x03 và status = 1.")
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

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Cần ít nhất 1 private key để test")
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		log.Fatalf("❌ Parse private key thất bại: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// Địa chỉ nhận
	var toAddr common.Address
	if len(cfg.PrivateKeys) > 1 {
		pk1, _ := crypto.HexToECDSA(cfg.PrivateKeys[1])
		toAddr = crypto.PubkeyToAddress(*pk1.Public().(*ecdsa.PublicKey))
	} else {
		toAddr = common.HexToAddress("0x0000000000000000000000000000000000000001")
	}

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			log.Fatalf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
	if err != nil {
		log.Fatalf("❌ Lấy nonce thất bại: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(20000000000) // 20 gwei
	}
	gasTipCap, err := client.SuggestGasTipCap(context.Background())
	if err != nil {
		gasTipCap = big.NewInt(1000000000) // 1 gwei
	}

	fmt.Printf("🔑 Sender: %s (Nonce: %d)\n", fromAddr.Hex(), nonce)
	fmt.Printf("📬 Receiver: %s\n", toAddr.Hex())
	fmt.Printf("🌐 ChainID: %s\n", chainID.String())

	// 1. Tạo KZG Blob, Commitment, Proof và Versioned Hash
	var blob kzg4844.Blob
	// Ghi một số bytes vào blob data (đảm bảo các element < BLS modulus)
	copy(blob[:], []byte("Metanode EIP-4844 Blob Transaction Test Data"))

	commitment, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		log.Fatalf("❌ BlobToCommitment thất bại: %v", err)
	}

	proof, err := kzg4844.ComputeBlobProof(&blob, commitment)
	if err != nil {
		log.Fatalf("❌ ComputeBlobProof thất bại: %v", err)
	}

	versionedHash := common.Hash(kzg4844.CalcBlobHashV1(sha256.New(), &commitment))
	fmt.Printf("📦 Blob Versioned Hash: %s\n", versionedHash.Hex())

	// 2. Cấu hình BlobTx
	blobTxData := &types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(gasTipCap),
		GasFeeCap:  uint256.MustFromBig(gasPrice),
		Gas:        210000,
		To:         toAddr,
		Data:       nil,
		BlobFeeCap: uint256.NewInt(1000000000), // 1 gwei max fee per blob gas
		BlobHashes: []common.Hash{versionedHash},
		Sidecar: &types.BlobTxSidecar{
			Blobs:       []kzg4844.Blob{blob},
			Commitments: []kzg4844.Commitment{commitment},
			Proofs:      []kzg4844.Proof{proof},
		},
	}

	// 3. Ký giao dịch với Prague/Cancun Signer
	signer := types.NewCancunSigner(chainID)
	signedTx, err := types.SignNewTx(pk0, signer, blobTxData)
	if err != nil {
		log.Fatalf("❌ Ký BlobTx thất bại: %v", err)
	}

	fmt.Printf("📤 Gửi BlobTx hash: %s (TxType: 0x%02x)...\n", signedTx.Hash().Hex(), signedTx.Type())

	// 4. Gửi qua RPC
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("❌ Lỗi gửi BlobTx qua RPC: %v", err)
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
	fmt.Printf("   - Tx Type: 0x%02x (Kỳ vọng: 0x03)\n", receipt.Type)
	fmt.Printf("   - Gas Used: %d\n", receipt.GasUsed)
	fmt.Printf("   - Blob Gas Used: %d\n", receipt.BlobGasUsed)
	if receipt.BlobGasPrice != nil {
		fmt.Printf("   - Blob Gas Price: %s wei\n", receipt.BlobGasPrice.String())
	}

	if receipt.Status != 1 {
		fmt.Println("❌ TEST FAILED: Transaction Status != 1 (Reverted)")
		os.Exit(1)
	}

	if receipt.Type != types.BlobTxType {
		fmt.Printf("❌ TEST FAILED: TxType không khớp (nhận được 0x%02x, kỳ vọng 0x%02x)\n", receipt.Type, types.BlobTxType)
		os.Exit(1)
	}

	fmt.Println("\n🎉 TEST 25 (EIP-4844 BLOB TX) PASSED THÀNH CÔNG!")
}
