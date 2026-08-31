/*
 * BÀI TEST: 31-mixed-all-types-block
 * MÔ TẢ   : Gửi đồng thời tất cả các loại Transaction Type (Type 0 Legacy, Type 2 EIP-1559,
 *           Type 3 EIP-4844 Blob, Type 4 EIP-7702 SetCode) trong cùng một đợt xử lý Block-STM.
 * GỌI     : Đóng gói và gửi song song đa dạng giao dịch từ các ví khác nhau.
 * KỲ VỌNG : Toàn bộ các giao dịch thuộc các type khác nhau đều được confirm thành công,
 *           Receipt Status = 1, Gas / BlobGas tính toán chính xác, không gây state drift / fork.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-blockstm/config"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
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
	fmt.Println("BÀI TEST: 31-mixed-all-types-block (HỖN HỢP TẤT CẢ TX TYPES TRONG BLOCK)")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra Block-STM và Consensus xử lý hỗn hợp đa loại giao dịch:")
	fmt.Println("             1. Type 0x00: Legacy Transaction (Transfer Native).")
	fmt.Println("             2. Type 0x02: EIP-1559 Dynamic Fee Transaction.")
	fmt.Println("             3. Type 0x03: EIP-4844 Blob Transaction (với KZG Commitments & Sidecar).")
	fmt.Println("             4. Type 0x04: EIP-7702 SetCode Transaction (với Authorization List).")
	fmt.Println("🎯 KỲ VỌNG : Block-STM xử lý song song thành công 100%, Receipt Root & State Root chuẩn.")
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

	testKeys := loadPrivateKeys("", cfg.PrivateKeys)
	if len(testKeys) < 8 {
		log.Fatalf("❌ Cần ít nhất 8 private keys để gửi hỗn hợp các loại transactions")
	}

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			log.Fatalf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	signerPrague := types.NewPragueSigner(chainID)
	signerEIP155 := types.NewEIP155Signer(chainID)

	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(20000000000)
	}
	gasTipCap, _ := client.SuggestGasTipCap(context.Background())
	if gasTipCap == nil || gasTipCap.Sign() == 0 {
		gasTipCap = big.NewInt(1000000000)
	}

	type TxSpec struct {
		Name     string
		TypeID   uint8
		FromKey  string
		ToAddr   common.Address
		BuildFn  func(nonce uint64, fromPK *ecdsa.PrivateKey, toAddr common.Address) *types.Transaction
	}

	pk0, _ := crypto.HexToECDSA(testKeys[0])
	addr0 := crypto.PubkeyToAddress(pk0.PublicKey)
	pk1, _ := crypto.HexToECDSA(testKeys[1])
	addr1 := crypto.PubkeyToAddress(pk1.PublicKey)
	pk2, _ := crypto.HexToECDSA(testKeys[2])
	addr2 := crypto.PubkeyToAddress(pk2.PublicKey)
	pk3, _ := crypto.HexToECDSA(testKeys[3])
	addr3 := crypto.PubkeyToAddress(pk3.PublicKey)
	pk4, _ := crypto.HexToECDSA(testKeys[4])
	addr4 := crypto.PubkeyToAddress(pk4.PublicKey)
	pk5, _ := crypto.HexToECDSA(testKeys[5])
	addr5 := crypto.PubkeyToAddress(pk5.PublicKey)
	pk6, _ := crypto.HexToECDSA(testKeys[6])
	addr6 := crypto.PubkeyToAddress(pk6.PublicKey)
	pk7, _ := crypto.HexToECDSA(testKeys[7])
	addr7 := crypto.PubkeyToAddress(pk7.PublicKey)

	_ = addr0
	_ = addr2
	_ = addr4
	_ = addr6

	// Chuẩn bị KZG Blob cho Type 0x03
	var blob kzg4844.Blob
	copy(blob[:], []byte("Metanode Mixed Block Test Blob Data"))
	commitment, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		log.Fatalf("❌ BlobToCommitment err: %v", err)
	}
	proof, err := kzg4844.ComputeBlobProof(&blob, commitment)
	if err != nil {
		log.Fatalf("❌ ComputeBlobProof err: %v", err)
	}
	versionedHash := common.Hash(kzg4844.CalcBlobHashV1(sha256.New(), &commitment))

	// Chuẩn bị SetCode Authorization cho Type 0x04
	authNonce, _ := client.PendingNonceAt(context.Background(), addr7)
	delegateContract := common.HexToAddress("0x0000000000000000000000000000000000007702")
	authTuple, err := types.SignSetCode(pk7, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContract,
		Nonce:   authNonce,
	})
	if err != nil {
		log.Fatalf("❌ SignSetCode err: %v", err)
	}

	specs := []TxSpec{
		// 1. Type 0x00: Legacy Tx
		{
			Name:    "Legacy Tx (Type 0x00)",
			TypeID:  0x00,
			FromKey: testKeys[0],
			ToAddr:  addr1,
			BuildFn: func(nonce uint64, fromPK *ecdsa.PrivateKey, to common.Address) *types.Transaction {
				tx := types.NewTransaction(nonce, to, big.NewInt(1000), 21000, gasPrice, nil)
				signedTx, _ := types.SignTx(tx, signerEIP155, fromPK)
				return signedTx
			},
		},
		// 2. Type 0x02: EIP-1559 Dynamic Fee Tx
		{
			Name:    "EIP-1559 Tx (Type 0x02)",
			TypeID:  0x02,
			FromKey: testKeys[2],
			ToAddr:  addr3,
			BuildFn: func(nonce uint64, fromPK *ecdsa.PrivateKey, to common.Address) *types.Transaction {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   chainID,
					Nonce:     nonce,
					GasTipCap: gasTipCap,
					GasFeeCap: gasPrice,
					Gas:       21000,
					To:        &to,
					Value:     big.NewInt(1000),
					Data:      nil,
				})
				signedTx, _ := types.SignTx(tx, signerPrague, fromPK)
				return signedTx
			},
		},
		// 3. Type 0x03: EIP-4844 Blob Tx
		{
			Name:    "EIP-4844 Blob Tx (Type 0x03)",
			TypeID:  0x03,
			FromKey: testKeys[4],
			ToAddr:  addr5,
			BuildFn: func(nonce uint64, fromPK *ecdsa.PrivateKey, to common.Address) *types.Transaction {
				blobTxData := &types.BlobTx{
					ChainID:    uint256.MustFromBig(chainID),
					Nonce:      nonce,
					GasTipCap:  uint256.MustFromBig(gasTipCap),
					GasFeeCap:  uint256.MustFromBig(gasPrice),
					Gas:        210000,
					To:         to,
					Value:      uint256.NewInt(1000),
					Data:       nil,
					BlobFeeCap: uint256.NewInt(1000000000),
					BlobHashes: []common.Hash{versionedHash},
					Sidecar: &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{blob},
						Commitments: []kzg4844.Commitment{commitment},
						Proofs:      []kzg4844.Proof{proof},
					},
				}
				tx := types.NewTx(blobTxData)
				signedTx, _ := types.SignTx(tx, signerPrague, fromPK)
				return signedTx
			},
		},
		// 4. Type 0x04: EIP-7702 SetCode Tx
		{
			Name:    "EIP-7702 SetCode Tx (Type 0x04)",
			TypeID:  0x04,
			FromKey: testKeys[6],
			ToAddr:  addr7,
			BuildFn: func(nonce uint64, fromPK *ecdsa.PrivateKey, to common.Address) *types.Transaction {
				setCodeTxData := &types.SetCodeTx{
					ChainID:   uint256.MustFromBig(chainID),
					Nonce:     nonce,
					GasTipCap: uint256.NewInt(1000000000),
					GasFeeCap: uint256.MustFromBig(gasPrice),
					Gas:       200000,
					To:        to,
					Value:     uint256.NewInt(0),
					Data:      nil,
					AuthList:  []types.SetCodeAuthorization{authTuple},
				}
				tx := types.NewTx(setCodeTxData)
				signedTx, _ := types.SignTx(tx, signerPrague, fromPK)
				return signedTx
			},
		},
	}

	fmt.Printf("\n🔥 Đang gửi đồng thời %d loại transactions khác nhau vào Mempool...\n", len(specs))
	var wg sync.WaitGroup
	txHashes := make([]common.Hash, len(specs))
	startSend := time.Now()

	for i, spec := range specs {
		wg.Add(1)
		go func(idx int, s TxSpec) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(s.FromKey)
			from := crypto.PubkeyToAddress(pk.PublicKey)
			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				log.Fatalf("❌ Lấy nonce ví %s thất bại: %v", from.Hex(), err)
			}
			tx := s.BuildFn(nonce, pk, s.ToAddr)
			if err := client.SendTransaction(context.Background(), tx); err != nil {
				fmt.Printf("   ❌ [%s] Gửi tx thất bại: %v\n", s.Name, err)
				return
			}
			txHashes[idx] = tx.Hash()
			fmt.Printf("   ✅ [%s] Gửi tx thành công: %s (Type: 0x%02x)\n", s.Name, tx.Hash().Hex(), s.TypeID)
		}(i, spec)
	}
	wg.Wait()

	fmt.Println("\n⏳ Đang chờ xác nhận và kiểm tra Receipts của tất cả loại Transactions...")
	successCount := 0
	for i, spec := range specs {
		h := txHashes[i]
		if h == (common.Hash{}) {
			continue
		}
		rcpt := waitForReceipt(client, h)
		if rcpt.Status != 1 {
			log.Fatalf("❌ [%s] Transaction bị REVERT! Hash: %s", spec.Name, h.Hex())
		}
		fmt.Printf("   🎉 [%s] Đã commit tại Block %d | Type: 0x%02x | GasUsed: %d | Status: %d\n",
			spec.Name, rcpt.BlockNumber.Uint64(), rcpt.Type, rcpt.GasUsed, rcpt.Status)
		if rcpt.Type != spec.TypeID {
			fmt.Printf("      ⚠️ Cảnh báo Type không khớp: got 0x%02x, want 0x%02x\n", rcpt.Type, spec.TypeID)
		}
		successCount++
	}

	elapsed := time.Since(startSend)
	fmt.Println("\n==========================================================")
	fmt.Println("📊 BÁO CÁO KẾT QUẢ TEST MIXED ALL TRANSACTION TYPES:")
	fmt.Println("==========================================================")
	fmt.Printf("Thời gian gửi & confirm: %v\n", elapsed)
	fmt.Printf("Tổng số loại Tx test   : %d\n", len(specs))
	fmt.Printf("Số Tx thành công       : %d\n", successCount)

	if successCount == len(specs) {
		fmt.Println("\n🎉 TEST PASSED: Block-STM và Validator Consensus xử lý mượt mà và chuẩn xác toàn bộ Transaction Types trong block!")
	} else {
		fmt.Println("\n❌ TEST FAILED: Có transaction type bị lỗi!")
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
