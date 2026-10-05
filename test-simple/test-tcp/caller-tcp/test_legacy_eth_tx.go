//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	e_types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	client_tcp "tool-test/pkg/client-tcp"
	com_pkg "tool-test/pkg/client-tcp/common"
	tcp_config "tool-test/pkg/client-tcp/config"
	pb "tool-test/pkg/proto"
	mt_transaction "tool-test/pkg/transaction"
)

type Config struct {
	PrivateKey              string `json:"private_key"`
	ParentConnectionAddress string `json:"parent_connection_address"`
	ChainID                 uint64 `json:"chain_id"`
	ParentConnectionType    string `json:"parent_connection_type"`
	ParentAddress           string `json:"parent_address"`
}

func main() {
	// 1. Đọc config
	configFile := "config-local.json"
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Không đọc được config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Parse config error: %v", err)
	}

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKey, "0x"))
	if err != nil {
		log.Fatalf("HexToECDSA err: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	fmt.Printf("🔑 Address: %s (hex: %s)\n", fromAddr.Hex(), common.Bytes2Hex(fromAddr.Bytes()))

	// 2. Kết nối TCP Client
	clientCfg := &tcp_config.ClientConfig{
		PrivateKey_:             cfg.PrivateKey,
		ParentAddress:           cfg.ParentAddress,
		ParentConnectionAddress: cfg.ParentConnectionAddress,
		ParentConnectionType:    cfg.ParentConnectionType,
		ChainId:                 cfg.ChainID,
	}
	cli, err := client_tcp.NewClient(clientCfg)
	if err != nil {
		log.Fatalf("NewClient err: %v", err)
	}
	time.Sleep(1 * time.Second)

	// 3. Lấy AccountState
	as, err := cli.AccountState(fromAddr)
	if err != nil {
		log.Fatalf("AccountState err: %v", err)
	}
	fmt.Printf("📊 Account State hiện tại: Nonce=%d, Balance=%s, BLSKeyLen=%d\n",
		as.Nonce(), as.Balance().String(), len(as.PublicKeyBls()))

	// 4. Build giao dịch theo đúng kiểu RegisterBls (dùng e_types.NewTransaction, Legacy Type 0)
	// Ví dụ: chuyển 0 native coin cho chính mình (self-transfer 0)
	nonce := as.Nonce()
	toAddr := fromAddr
	amount := big.NewInt(0)
	gasLimit := com_pkg.DefaultMaxGas
	gasPrice := new(big.Int).SetUint64(com_pkg.DefaultMaxGasPrice)

	fmt.Printf("\n🔨 [BƯỚC 1] Tạo Ethereum Legacy Transaction (Type 0)...\n")
	ethTx := e_types.NewTransaction(nonce, toAddr, amount, gasLimit, gasPrice, nil)

	// Ký bằng EIP155Signer của go-ethereum (như CreateSignedSetBLSPublicKeyTx)
	chainID := new(big.Int).SetUint64(cfg.ChainID)
	signer := e_types.NewEIP155Signer(chainID)
	signedEthTx, err := e_types.SignTx(ethTx, signer, privKey)
	if err != nil {
		log.Fatalf("SignTx error: %v", err)
	}

	// 5. Convert sang pb.Transaction bằng NewTransactionFromEth
	fmt.Printf("🔨 [BƯỚC 2] Convert sang pb.Transaction bằng NewTransactionFromEth...\n")
	mtTx, err := mt_transaction.NewTransactionFromEth(signedEthTx)
	if err != nil {
		log.Fatalf("NewTransactionFromEth error: %v", err)
	}

	concreteTx, ok := mtTx.(*mt_transaction.Transaction)
	if !ok {
		log.Fatalf("Type assertion to *mt_transaction.Transaction failed")
	}

	v, r, s := signedEthTx.RawSignatureValues()
	fmt.Printf("   - Tx Type: %d (Legacy)\n", concreteTx.Type())
	fmt.Printf("   - Nonce: %d\n", concreteTx.GetNonce())
	fmt.Printf("   - FromAddress: %s\n", concreteTx.FromAddress().Hex())
	fmt.Printf("   - ToAddress: %s\n", concreteTx.ToAddress().Hex())
	fmt.Printf("   - R: %x, S: %x, V: %s\n", r.Bytes(), s.Bytes(), v.String())
	fmt.Printf("   - Sign (BLS) trước: %x (len: %d)\n", concreteTx.Sign().Bytes(), len(concreteTx.Sign().Bytes()))

	// Test case 1: Xem Node có nhận không khi KHÔNG có chữ ký BLS
	fmt.Println("\n🚀 [THỬ NGHIỆM 1] Gửi giao dịch Type 0 (chỉ có chữ ký ETH, không SetSign BLS)...")
	sentTx, err := cli.GetTransactionController().SendNewTransaction(concreteTx)
	if err != nil {
		fmt.Printf("❌ SendNewTransaction thất bại ngay: %v\n", err)
		return
	}
	fmt.Printf("   Gửi thành công txHash: %s. Đang đợi receipt (tối đa 15s)...\n", sentTx.Hash().Hex())
	receipt, err := cli.FindReceiptByHash(sentTx.Hash())
	if err != nil {
		fmt.Printf("   ❌ Đợi receipt thất bại: %v\n", err)
	} else {
		fmt.Printf("   🎉 THÀNH CÔNG! Receipt nhận được: Status: %d (%s), GasUsed: %d\n",
			receipt.Status(), pb.RECEIPT_STATUS_name[int32(receipt.Status())], receipt.GasUsed())
	}
}
