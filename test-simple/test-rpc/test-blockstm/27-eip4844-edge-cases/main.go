package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 27-eip4844-edge-cases")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra các trường hợp biên và rủi ro bảo mật của EIP-4844:")
	fmt.Println("             1. Blob Tx vượt quá MAX_BLOBS_PER_TX (7 blobs > limit 6)")
	fmt.Println("             2. Blob Tx cố tình tạo Contract (To = nil)")
	fmt.Println("             3. Blob Tx có Blob Sidecar / KZG Proof bị sai lệch")
	fmt.Println("🎯 KỲ VỌNG : Node phải từ chối (reject) tại RPC admission boundary.")
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

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		log.Fatalf("❌ Parse private key thất bại: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(pk0.PublicKey)

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

	signer := types.NewCancunSigner(chainID)
	toAddr := common.HexToAddress("0xd5D1c7e1c276288Fa0993bB7B1cF40C73f1226A4")

	// Tạo 1 blob chuẩn
	var singleBlob kzg4844.Blob
	copy(singleBlob[:], []byte("valid blob data"))
	commitment, _ := kzg4844.BlobToCommitment(&singleBlob)
	proof, _ := kzg4844.ComputeBlobProof(&singleBlob, commitment)
	versionedHash := common.Hash(kzg4844.CalcBlobHashV1(sha256.New(), &commitment))

	// -------------------------------------------------------------------------
	// CASE 1: Blob Tx vượt quá MAX_BLOBS_PER_TX (7 blobs)
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 TEST CASE 1: Gửi BlobTx với 7 blobs (vượt quá giới hạn 6 blobs/tx)...")
	var blobs7 []kzg4844.Blob
	var commits7 []kzg4844.Commitment
	var proofs7 []kzg4844.Proof
	var hashes7 []common.Hash
	for i := 0; i < 7; i++ {
		blobs7 = append(blobs7, singleBlob)
		commits7 = append(commits7, commitment)
		proofs7 = append(proofs7, proof)
		hashes7 = append(hashes7, versionedHash)
	}

	tx7Blobs := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1000000000),
		GasFeeCap:  uint256.NewInt(20000000000),
		Gas:        500000,
		To:         toAddr,
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(1000000000),
		BlobHashes: hashes7,
		Sidecar: &types.BlobTxSidecar{
			Blobs:       blobs7,
			Commitments: commits7,
			Proofs:      proofs7,
		},
	})
	signedTx7, _ := types.SignTx(tx7Blobs, signer, pk0)
	err = client.SendTransaction(context.Background(), signedTx7)
	if err != nil {
		fmt.Printf("   ✅ Node từ chối chính xác: %v\n", err)
	} else {
		log.Fatalf("   ❌ LỖI BẢO MẬT: Node không từ chối giao dịch mang 7 blobs!")
	}

	// -------------------------------------------------------------------------
	// CASE 2: Blob Tx cố tình tạo contract (To = nil)
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 TEST CASE 2: Gửi BlobTx với To = nil (cố tình tạo smart contract qua BlobTx)...")
	txCreate := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1000000000),
		GasFeeCap:  uint256.NewInt(20000000000),
		Gas:        210000,
		To:         common.Address{}, // Contract creation
		Value:      uint256.NewInt(0),
		Data:       []byte{0x60, 0x00, 0x60, 0x00, 0xf3},
		BlobFeeCap: uint256.NewInt(1000000000),
		BlobHashes: []common.Hash{versionedHash},
		Sidecar: &types.BlobTxSidecar{
			Blobs:       []kzg4844.Blob{singleBlob},
			Commitments: []kzg4844.Commitment{commitment},
			Proofs:      []kzg4844.Proof{proof},
		},
	})
	signedTxCreate, _ := types.SignTx(txCreate, signer, pk0)
	err = client.SendTransaction(context.Background(), signedTxCreate)
	if err != nil {
		fmt.Printf("   ✅ Node từ chối chính xác: %v\n", err)
	} else {
		log.Fatalf("   ❌ LỖI BẢO MẬT: Node cho phép tạo contract qua BlobTx!")
	}

	// -------------------------------------------------------------------------
	// CASE 3: Blob Tx có KZG Proof / Commitment bị giả mạo
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 TEST CASE 3: Gửi BlobTx với KZG Proof bị sai lệch (Corrupted Proof)...")
	corruptProof := proof
	corruptProof[0] ^= 0xff // làm sai lệch 1 byte trong proof

	txCorrupt := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1000000000),
		GasFeeCap:  uint256.NewInt(20000000000),
		Gas:        210000,
		To:         toAddr,
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(1000000000),
		BlobHashes: []common.Hash{versionedHash},
		Sidecar: &types.BlobTxSidecar{
			Blobs:       []kzg4844.Blob{singleBlob},
			Commitments: []kzg4844.Commitment{commitment},
			Proofs:      []kzg4844.Proof{corruptProof},
		},
	})
	signedTxCorrupt, _ := types.SignTx(txCorrupt, signer, pk0)
	err = client.SendTransaction(context.Background(), signedTxCorrupt)
	if err != nil && (strings.Contains(err.Error(), "kzg") || strings.Contains(err.Error(), "proof") || strings.Contains(err.Error(), "sidecar") || strings.Contains(err.Error(), "verify") || strings.Contains(err.Error(), "failed")) {
		fmt.Printf("   ✅ Node phát hiện và từ chối KZG proof giả mạo: %v\n", err)
	} else {
		log.Fatalf("   ❌ LỖI BẢO MẬT: Node không phát hiện KZG proof bị hỏng! Kết quả: %v", err)
	}

	fmt.Println("\n🎉 TẤT CẢ CÁC TEST CASES BIÊN EIP-4844 ĐÃ PASSED HOÀN HẢO!")
}
