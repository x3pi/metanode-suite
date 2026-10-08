package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
)

func RunTest(configPath string) error {
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

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("❌ Lỗi kết nối RPC: %v", err)
	}
	defer client.Close()

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("❌ Parse private key thất bại: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(pk0.PublicKey)

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce thất bại: %v", err)
	}

	signer := types.NewCancunSigner(chainID)
	toAddr := common.HexToAddress("0xd5D1c7e1c276288Fa0993bB7B1cF40C73f1226A4")

	// Tạo 1 blob chuẩn
	var singleBlob kzg4844.Blob
	// Keep the high byte zero so the first field element is canonical.
	copy(singleBlob[1:32], []byte("valid blob data"))
	commitment, err := kzg4844.BlobToCommitment(&singleBlob)
	if err != nil {
		return fmt.Errorf("build commitment: %w", err)
	}
	proof, err := kzg4844.ComputeBlobProof(&singleBlob, commitment)
	if err != nil {
		return fmt.Errorf("build proof: %w", err)
	}
	if err := kzg4844.VerifyBlobProof(&singleBlob, commitment, proof); err != nil {
		return fmt.Errorf("invalid baseline proof: %w", err)
	}
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
	signedTx7, err := types.SignTx(tx7Blobs, signer, pk0)
	if err != nil {
		return err
	}
	err = client.SendTransaction(context.Background(), signedTx7)
	var rpcErr rpc.Error
	if err != nil && errors.As(err, &rpcErr) && rpcErr.ErrorCode() == -32000 {
		fmt.Printf("   ✅ Node từ chối chính xác: %v\n", err)
	} else {
		return fmt.Errorf("   ❌ LỖI BẢO MẬT: Node không từ chối giao dịch mang 7 blobs!")
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
		To:         toAddr, // Replaced with an empty RLP recipient below.
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
	signedTxCreate, err := types.SignTx(txCreate, signer, pk0)
	if err != nil {
		return err
	}
	// BlobTx.To is a value, so a nil recipient must be encoded manually.
	raw, err := signedTxCreate.WithoutBlobTxSidecar().MarshalBinary()
	if err != nil {
		return err
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(raw[1:], &fields); err != nil {
		return err
	}
	fields[5] = rlp.RawValue{0x80}
	payload, err := rlp.EncodeToBytes(fields)
	if err != nil {
		return err
	}
	var result common.Hash
	err = client.Client().CallContext(context.Background(), &result, "eth_sendRawTransaction", hexutil.Encode(append([]byte{types.BlobTxType}, payload...)))
	if err != nil && errors.As(err, &rpcErr) && rpcErr.ErrorCode() == -32000 && strings.Contains(strings.ToLower(err.Error()), "decode") {
		fmt.Printf("   ✅ Node từ chối chính xác: %v\n", err)
	} else {
		return fmt.Errorf("   ❌ LỖI BẢO MẬT: Node cho phép tạo contract qua BlobTx!")
	}

	// -------------------------------------------------------------------------
	// CASE 3: Blob Tx có KZG Proof / Commitment bị giả mạo
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 TEST CASE 3: Gửi BlobTx với KZG Proof bị sai lệch (Corrupted Proof)...")
	corruptProof := proof
	corruptProof[0] ^= 0xff // làm sai lệch 1 byte trong proof
	if err := kzg4844.VerifyBlobProof(&singleBlob, commitment, corruptProof); err == nil {
		return fmt.Errorf("corrupted proof unexpectedly passed local verification")
	}

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
	signedTxCorrupt, err := types.SignTx(txCorrupt, signer, pk0)
	if err != nil {
		return err
	}
	err = client.SendTransaction(context.Background(), signedTxCorrupt)
	if err != nil && errors.As(err, &rpcErr) && rpcErr.ErrorCode() == -32000 && strings.Contains(strings.ToLower(err.Error()), "kzg proof verification failed") {
		fmt.Printf("   ✅ Node phát hiện và từ chối KZG proof giả mạo: %v\n", err)
	} else {
		return fmt.Errorf("expected explicit KZG rejection; got %v (nil means accepted; another error does not establish KZG verification)", err)
	}

	fmt.Println("\n🎉 TẤT CẢ CÁC TEST CASES BIÊN EIP-4844 ĐÃ PASSED HOÀN HẢO!")
	return nil
}

func main() {
	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	if err := RunTest(configPath); err != nil {
		log.Fatalf("%v", err)
	}
}
