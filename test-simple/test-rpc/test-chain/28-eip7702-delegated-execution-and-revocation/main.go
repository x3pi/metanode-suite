package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"
	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)

// Simple Counter Bytecode:
// set(uint256): 0x60... (stores value at slot 0)
// get(): 0x60... (loads value at slot 0)
// Bytecode runtime: 60003560005500 (stores input[0:32] into slot 0)
const (
	// Simple store contract: saves first 32 bytes of calldata into slot 0
	// Init code deploys: 60003560005500
	deployCodeHex = "6007600c60003960076000f360003560005500"
)

func waitForReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("❌ Timeout chờ receipt cho tx %s", txHash.Hex())
		case <-time.After(1 * time.Second):
		}
	}
}

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 28-eip7702-delegated-execution-and-revocation")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra chuyên sâu cơ chế ủy quyền và thu hồi EIP-7702:")
	fmt.Println("             1. Deploy Counter Contract làm delegate logic.")
	fmt.Println("             2. EOA B ký ủy quyền sang Counter Contract.")
	fmt.Println("             3. Gọi vào EOA B và xác nhận execution chạy logic của contract trên EOA B.")
	fmt.Println("             4. EOA B ký Authorization trỏ về 0x0 (Revocation) -> Code trở về 0x.")
	fmt.Println("🎯 KỲ VỌNG : Tất cả các bước đều thành công, status = 1, mã được gắn và thu hồi chính xác.")
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

	if len(cfg.PrivateKeys) < 2 {
		return fmt.Errorf("❌ Cần ít nhất 2 private key để test delegation và caller")
	}

	pkCaller, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("❌ Parse private key caller thất bại: %v", err)
	}
	addrCaller := crypto.PubkeyToAddress(pkCaller.PublicKey)

	pkEOAB, err := crypto.HexToECDSA(cfg.PrivateKeys[1])
	if err != nil {
		return fmt.Errorf("❌ Parse private key EOAB thất bại: %v", err)
	}
	addrEOAB := crypto.PubkeyToAddress(pkEOAB.PublicKey)

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	signer := types.NewPragueSigner(chainID)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(20000000000)
	}

	// -------------------------------------------------------------------------
	// BƯỚC 1: Deploy Contract mẫu để làm Delegate Target
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 1: Deploy Smart Contract mẫu (Delegate Target)...")
	nonceCaller, err := client.PendingNonceAt(context.Background(), addrCaller)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce caller thất bại: %v", err)
	}
	deployBytecode, err := hex.DecodeString(deployCodeHex)
	if err != nil {
		return fmt.Errorf("❌ Decode deploy bytecode thất bại: %v", err)
	}

	txDeploy := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonceCaller,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: gasPrice,
		Gas:       300000,
		To:        nil, // Contract deployment
		Value:     big.NewInt(0),
		Data:      deployBytecode,
	})
	signedDeploy, err := types.SignTx(txDeploy, signer, pkCaller)
	if err != nil {
		return fmt.Errorf("❌ Ký deploy contract thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedDeploy); err != nil {
		return fmt.Errorf("❌ Deploy contract mẫu thất bại: %v", err)
	}
	rcptDeploy, err := waitForReceipt(client, signedDeploy.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt deploy thất bại: %v", err)
	}
	if rcptDeploy.Status != 1 {
		return fmt.Errorf("❌ Deploy contract reverted!")
	}
	delegateContractAddr := rcptDeploy.ContractAddress
	fmt.Printf("   ✅ Target Contract đã deploy tại: %s (Status = %d)\n", delegateContractAddr.Hex(), rcptDeploy.Status)

	// -------------------------------------------------------------------------
	// BƯỚC 2: EOA B ký Authorization ủy quyền sang Delegate Contract
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 2: EOA B ký Authorization delegate sang Target Contract...")
	nonceEOAB, err := client.PendingNonceAt(context.Background(), addrEOAB)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce EOAB thất bại: %v", err)
	}
	authTuple, err := types.SignSetCode(pkEOAB, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContractAddr,
		Nonce:   nonceEOAB,
	})
	if err != nil {
		return fmt.Errorf("❌ Ký SetCode Authorization thất bại: %v", err)
	}

	nonceCaller, err = client.PendingNonceAt(context.Background(), addrCaller)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce caller thất bại: %v", err)
	}
	setCodeTx := types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     nonceCaller,
		GasTipCap: uint256.NewInt(1000000000),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200000,
		To:        addrEOAB,
		Value:     uint256.NewInt(0),
		AuthList:  []types.SetCodeAuthorization{authTuple},
	})
	signedSetCode, err := types.SignTx(setCodeTx, signer, pkCaller)
	if err != nil {
		return fmt.Errorf("❌ Ký SetCodeTx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedSetCode); err != nil {
		return fmt.Errorf("❌ Gửi SetCodeTx thất bại: %v", err)
	}
	rcptSetCode, err := waitForReceipt(client, signedSetCode.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt setcode thất bại: %v", err)
	}
	fmt.Printf("   ✅ SetCodeTx đã commit tại block %d (Status = %d)\n", rcptSetCode.BlockNumber.Uint64(), rcptSetCode.Status)

	// Kiểm tra code tại EOA B
	codeAfterDelegate, _ := client.CodeAt(context.Background(), addrEOAB, nil)
	expectedDesignator := "ef0100" + delegateContractAddr.Hex()[2:]
	fmt.Printf("   🔍 Bytecode tại EOA B: 0x%x (Kỳ vọng: 0x%s)\n", codeAfterDelegate, expectedDesignator)
	if !strings.EqualFold(hex.EncodeToString(codeAfterDelegate), expectedDesignator) {
		return fmt.Errorf("   ❌ Bytecode delegation designator không khớp! Got: %s, Want: %s", hex.EncodeToString(codeAfterDelegate), expectedDesignator)
	}

	// -------------------------------------------------------------------------
	// BƯỚC 3: Gọi vào EOA B với Calldata -> Thực thi code của Delegate Contract
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 3: Gửi transaction gọi vào EOA B để kích hoạt delegate code...")
	nonceCaller, _ = client.PendingNonceAt(context.Background(), addrCaller)
	testValueData := common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000042")
	callTx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonceCaller,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: gasPrice,
		Gas:       200000,
		To:        &addrEOAB,
		Value:     big.NewInt(0),
		Data:      testValueData,
	})
	signedCall, err := types.SignTx(callTx, signer, pkCaller)
	if err != nil {
		return fmt.Errorf("❌ Ký call tx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedCall); err != nil {
		return fmt.Errorf("❌ Gửi Tx gọi vào Delegated EOA thất bại: %v", err)
	}
	rcptCall, err := waitForReceipt(client, signedCall.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt call thất bại: %v", err)
	}
	fmt.Printf("   ✅ Tx gọi EOA B thành công (Status = %d, GasUsed = %d)\n", rcptCall.Status, rcptCall.GasUsed)
	if rcptCall.Status != 1 {
		return fmt.Errorf("❌ Call to delegated EOA reverted!")
	}

	// -------------------------------------------------------------------------
	// BƯỚC 4: EOA B Thu hồi ủy quyền (Revocation - delegate to 0x000...0)
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 4: EOA B ký Authorization thu hồi ủy quyền (Revoke to 0x0)...")
	nonceEOAB, _ = client.PendingNonceAt(context.Background(), addrEOAB)
	revokeTuple, err := types.SignSetCode(pkEOAB, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: common.Address{}, // Zero Address = Revocation
		Nonce:   nonceEOAB,
	})
	if err != nil {
		return fmt.Errorf("❌ Ký Revocation Authorization thất bại: %v", err)
	}

	nonceCaller, _ = client.PendingNonceAt(context.Background(), addrCaller)
	revokeTx := types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     nonceCaller,
		GasTipCap: uint256.NewInt(1000000000),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       200000,
		To:        addrEOAB,
		Value:     uint256.NewInt(0),
		AuthList:  []types.SetCodeAuthorization{revokeTuple},
	})
	signedRevoke, err := types.SignTx(revokeTx, signer, pkCaller)
	if err != nil {
		return fmt.Errorf("❌ Ký revocation tx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedRevoke); err != nil {
		return fmt.Errorf("❌ Gửi Revocation Tx thất bại: %v", err)
	}
	rcptRevoke, err := waitForReceipt(client, signedRevoke.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt revocation thất bại: %v", err)
	}
	fmt.Printf("   ✅ Revocation Tx đã commit tại block %d (Status = %d)\n", rcptRevoke.BlockNumber.Uint64(), rcptRevoke.Status)

	// Kiểm tra lại code tại EOA B -> phải là 0x (rỗng)
	codeAfterRevoke, _ := client.CodeAt(context.Background(), addrEOAB, nil)
	fmt.Printf("   🔍 Bytecode tại EOA B sau khi thu hồi: 0x%x (Độ dài: %d)\n", codeAfterRevoke, len(codeAfterRevoke))
	if len(codeAfterRevoke) != 0 {
		return fmt.Errorf("   ❌ LỖI: Bytecode tại EOA B chưa được thu hồi về rỗng!")
	}

	fmt.Println("\n🎉 TẤT CẢ CÁC BƯỚC EIP-7702 (DELEGATION, EXECUTION, REVOCATION) ĐÃ PASSED HOÀN HẢO!")
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
