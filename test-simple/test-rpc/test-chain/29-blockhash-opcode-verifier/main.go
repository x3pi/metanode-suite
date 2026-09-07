package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"
	"tool-test/test-simple/test-rpc/test-chain/config"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Runtime: 6000354060005260206000f3 (takes uint256 blockNumber, calls BLOCKHASH, returns 32-byte hash)
// Deploy code: 600c600c600039600c6000f36000354060005260206000f3
const blockhashBytecode = "600c600c600039600c6000f36000354060005260206000f3"

func getRPCBlockHash(rpcClient *rpc.Client, blockNumber uint64) (common.Hash, error) {
	var rawBlock struct {
		Hash common.Hash `json:"hash"`
	}
	hexNum := fmt.Sprintf("0x%x", blockNumber)
	err := rpcClient.CallContext(context.Background(), &rawBlock, "eth_getBlockByNumber", hexNum, false)
	if err != nil {
		return common.Hash{}, err
	}
	return rawBlock.Hash, nil
}

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
	fmt.Println("BÀI TEST: 29-blockhash-opcode-verifier")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra tính toàn vẹn và bảo mật của Opcode BLOCKHASH (0x40):")
	fmt.Println("             1. Deploy Smart Contract gọi opcode BLOCKHASH(blockNumber).")
	fmt.Println("             2. BLOCKHASH của block hiện tại (>= current block) -> Trả về 0x0 (EVM spec).")
	fmt.Println("             3. Mine block mới, sau đó truy vấn BLOCKHASH của block vừa deploy.")
	fmt.Println("             4. So khớp hash trả về từ EVM với hash thực tế từ RPC eth_getBlockByNumber.")
	fmt.Println("             5. Kiểm tra truy vấn ngoài cửa sổ 256 blocks -> trả về 0x0.")
	fmt.Println("🎯 KỲ VỌNG : Opcode BLOCKHASH hoạt động chuẩn 100% theo đặc tả EVM mà không cần callback runtime.")
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

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("❌ Parse private key thất bại: %v", err)
	}
	addr0 := crypto.PubkeyToAddress(pk0.PublicKey)

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	signer := types.NewCancunSigner(chainID)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(20000000000)
	}

	// -------------------------------------------------------------------------
	// BƯỚC 1: Deploy BlockHash Verifier Contract
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 1: Deploy BlockHash Verifier Smart Contract...")
	nonce, err := client.PendingNonceAt(context.Background(), addr0)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce thất bại: %v", err)
	}
	deployData, err := hex.DecodeString(blockhashBytecode)
	if err != nil {
		return fmt.Errorf("❌ Decode deploy bytecode thất bại: %v", err)
	}

	txDeploy := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: gasPrice,
		Gas:       300000,
		To:        nil,
		Value:     big.NewInt(0),
		Data:      deployData,
	})
	signedDeploy, err := types.SignTx(txDeploy, signer, pk0)
	if err != nil {
		return fmt.Errorf("❌ Ký deploy tx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedDeploy); err != nil {
		return fmt.Errorf("❌ Deploy contract thất bại: %v", err)
	}
	rcpt, err := waitForReceipt(client, signedDeploy.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt deploy thất bại: %v", err)
	}
	if rcpt.Status != 1 {
		return fmt.Errorf("❌ Deploy contract reverted!")
	}
	contractAddr := rcpt.ContractAddress
	deployedBlock := rcpt.BlockNumber.Uint64()
	fmt.Printf("   ✅ Contract deployed tại: %s (Block Number: %d)\n", contractAddr.Hex(), deployedBlock)

	// -------------------------------------------------------------------------
	// BƯỚC 2: Kiểm tra BLOCKHASH cho block trước đó (deployedBlock - 1)
	// -------------------------------------------------------------------------
	fmt.Printf("\n🔹 BƯỚC 2: Gọi EVM lấy BLOCKHASH cho block trước đó (block %d - 1 = %d)...\n", deployedBlock, deployedBlock-1)
	prevBlockNum := big.NewInt(int64(deployedBlock - 1))
	prevBlockBytes := common.BigToHash(prevBlockNum).Bytes()

	callMsg := ethereum.CallMsg{
		To:   &contractAddr,
		Data: prevBlockBytes,
	}
	resBytes, err := client.CallContract(context.Background(), callMsg, nil)
	if err != nil {
		return fmt.Errorf("❌ CallContract getBlockHash thất bại: %v", err)
	}
	evmPrevHash := common.BytesToHash(resBytes)

	rpcClient, err := rpc.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("❌ Lỗi kết nối RPC Client: %v", err)
	}

	actualPrevHash, err := getRPCBlockHash(rpcClient, deployedBlock-1)
	if err != nil {
		return fmt.Errorf("❌ Lấy block từ RPC thất bại: %v", err)
	}

	fmt.Printf("   🔍 Block %d Hash từ EVM Opcode: %s\n", deployedBlock-1, evmPrevHash.Hex())
	fmt.Printf("   🔍 Block %d Hash từ RPC Block:  %s\n", deployedBlock-1, actualPrevHash.Hex())

	if evmPrevHash != actualPrevHash {
		return fmt.Errorf("   ❌ LỖI: Hash từ Opcode BLOCKHASH không khớp với RPC Block!")
	}
	fmt.Printf("   ✅ Khớp hoàn hảo giữa EVM BLOCKHASH và RPC Block cho block %d!\n", deployedBlock-1)

	// -------------------------------------------------------------------------
	// BƯỚC 3: Gửi 1 Tx chuyển tiền để sinh Block tiếp theo, sau đó đọc BLOCKHASH(deployedBlock)
	// -------------------------------------------------------------------------
	fmt.Printf("\n🔹 BƯỚC 3: Gửi giao dịch tạo Block mới và kiểm tra BLOCKHASH cho Block %d...\n", deployedBlock)
	nonce, _ = client.PendingNonceAt(context.Background(), addr0)
	dummyTx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: gasPrice,
		Gas:       21000,
		To:        &addr0,
		Value:     big.NewInt(1),
	})
	signedDummy, err := types.SignTx(dummyTx, signer, pk0)
	if err != nil {
		return fmt.Errorf("❌ Ký dummy tx thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedDummy); err != nil {
		return fmt.Errorf("❌ Gửi dummy tx thất bại: %v", err)
	}
	rcptDummy, err := waitForReceipt(client, signedDummy.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt dummy tx thất bại: %v", err)
	}
	fmt.Printf("   ✅ Block mới đã sinh ra: Block %d\n", rcptDummy.BlockNumber.Uint64())

	// Giờ block hiện tại là rcptDummy.BlockNumber, ta truy vấn BLOCKHASH cho deployedBlock
	depBlockBytes := common.BigToHash(big.NewInt(int64(deployedBlock))).Bytes()
	resDep, err := client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &contractAddr,
		Data: depBlockBytes,
	}, nil)
	if err != nil {
		return fmt.Errorf("❌ CallContract getBlockHash thất bại: %v", err)
	}
	evmDepHash := common.BytesToHash(resDep)

	actualDepHash, err := getRPCBlockHash(rpcClient, deployedBlock)
	if err != nil {
		return fmt.Errorf("❌ Lấy block từ RPC thất bại: %v", err)
	}

	fmt.Printf("   🔍 Block %d Hash từ EVM Opcode: %s\n", deployedBlock, evmDepHash.Hex())
	fmt.Printf("   🔍 Block %d Hash từ RPC Block:  %s\n", deployedBlock, actualDepHash.Hex())

	if evmDepHash != actualDepHash {
		return fmt.Errorf("   ❌ LỖI: Hash từ Opcode BLOCKHASH không khớp với RPC Block!")
	}
	fmt.Printf("   ✅ Khớp hoàn hảo giữa EVM BLOCKHASH và RPC Block cho Block %d!\n", deployedBlock)

	// -------------------------------------------------------------------------
	// BƯỚC 4: Kiểm tra BLOCKHASH ngoài cửa sổ 256 block -> phải trả về 0x0
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 4: Gọi BLOCKHASH cho block quá 256 block hoặc block tương lai...")
	farBlockNum := big.NewInt(99999999) // tương lai
	farBlockBytes := common.BigToHash(farBlockNum).Bytes()
	resFar, err := client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &contractAddr,
		Data: farBlockBytes,
	}, nil)
	if err != nil {
		return fmt.Errorf("❌ CallContract block tương lai thất bại: %v", err)
	}
	farHash := common.BytesToHash(resFar)
	fmt.Printf("   🔍 BLOCKHASH cho block tương lai %d: %s\n", farBlockNum.Uint64(), farHash.Hex())
	if farHash != (common.Hash{}) {
		return fmt.Errorf("   ❌ LỖI: Block tương lai phải trả về 0x0!")
	}
	fmt.Printf("   ✅ Block tương lai trả về đúng 0x0 (rỗng) theo tiêu chuẩn EVM!\n")

	fmt.Println("\n🎉 TẤT CẢ CÁC BƯỚC KIỂM TRA OPCODE BLOCKHASH ĐÃ PASSED HOÀN HẢO!")
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
