package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	e_types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"tool-test/pkg/bls"
	"tool-test/register_bls/rpc"
)

// waitReceiptPollHTTP đợi receipt qua HTTP RPC poll
func waitReceiptPollHTTP(rpcClient *rpc.RPCClient, txHash string) *rpc.Receipt {
	if txHash == "" {
		return nil
	}
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		receipt, err := rpcClient.GetTransactionReceipt(txHash)
		if err != nil {
			fmt.Printf("  ❌ Receipt error: %v\n", err)
			return nil
		}
		if receipt != nil {
			// Xóa dòng log này để đỡ trôi terminal khi chạy 1000 luồng
			return receipt
		}
		select {
		case <-timer.C:
			fmt.Println("  ⚠️ Timeout waiting for receipt")
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// sendTxAndWaitHTTP tạo, ký, gửi transaction + đợi receipt qua HTTP
func sendTxAndWaitHTTP(
	rpcClient *rpc.RPCClient,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) (string, *rpc.Receipt) {
	nonce, err := rpcClient.GetNonce(fromAddr.Hex())
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		os.Exit(1)
	}

	inputData, err := parsedABI.Pack(method, args...)
	if err != nil {
		fmt.Printf("  ❌ ABI pack error (%s): %v\n", method, err)
		os.Exit(1)
	}

	tx := e_types.NewTransaction(nonce, toAddr, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, err := e_types.SignTx(tx, signer, privKey)
	if err != nil {
		fmt.Printf("  ❌ SignTx error: %v\n", err)
		os.Exit(1)
	}

	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, err := rpcClient.SendRawTransaction(rawTxHex)
	if err != nil {
		// Chỉ log nếu có lỗi
		fmt.Printf("  ❌ SendRawTransaction error: %v\n", err)
		os.Exit(1)
	}
	// Đã xóa log fmt.Printf("  ✅ txHash: %s\n", txHash)

	receipt := waitReceiptPollHTTP(rpcClient, txHash)
	return txHash, receipt
}

func testBlsRegistrationHTTP(
	rpcClient *rpc.RPCClient,
	accountABI abi.ABI,
	accountContract common.Address,
	adminPrivKey *ecdsa.PrivateKey,
	adminAddr common.Address,
	signer e_types.Signer,
	count int,
	outFile string,
) {
	bls.Init()
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Printf("║  TEST: BLS Registration HTTP + Confirm (%d keys)      ║\n", count)
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	type KeyGenResult struct {
		Index         int    `json:"index"`
		Address       string `json:"address"`
		PrivateKey    string `json:"private_key"`
		BlsPubKey     string `json:"bls_pub_key"`
		BlsPrivateKey string `json:"bls_private_key"`
	}
	var results []KeyGenResult

	// === LẤY BLS PUBLIC KEY 1 LẦN DÙNG CHUNG CHO TẤT CẢ ===
	getPubKeyInput, _ := accountABI.Pack("getPublickeyBls")
	blsPubKeyResult, err := rpcClient.EthCall(accountContract.Hex(), "0x"+hex.EncodeToString(getPubKeyInput))
	if err != nil {
		fmt.Printf("❌ Lỗi getPublickeyBls RpcEthCall: %v\n", err)
		return
	}

	var blsPubKey []byte
	strRes := string(blsPubKeyResult)
	strRes = strings.Trim(strRes, "\"")
	strRes = strings.TrimPrefix(strRes, "0x")
	decoded, errHex := hex.DecodeString(strRes)
	if errHex == nil && len(decoded) >= 48 {
		blsPubKey = decoded
	} else {
		err = accountABI.UnpackIntoInterface(&blsPubKey, "getPublickeyBls", blsPubKeyResult)
		if err != nil {
			fmt.Printf("❌ Lỗi Decode getPublickeyBls: %v\n", err)
			return
		}
	}

	serverBlsPubKey := "0x" + hex.EncodeToString(blsPubKey)
	fmt.Printf("✅ Fetched BLS PublicKey (once): %s (%d bytes)\n", serverBlsPubKey, len(blsPubKey))

	if len(blsPubKey) == 0 {
		fmt.Println("❌ BLS pubkey rỗng, không thể tiếp tục.")
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 1000) // Giới hạn 500 request đồng thời
	completedCount := 0

	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // Release

			// Lược bớt log để console đỡ bị trôi khi chạy song song 1000 luồng
			// Step 1: Tạo private key mới
			newPrivKey, err := crypto.GenerateKey()
			if err != nil {
				fmt.Printf("  ❌ GenerateKey: %v\n", err)
				os.Exit(1)
			}
			newAddr := crypto.PubkeyToAddress(newPrivKey.PublicKey)
			newPrivKeyHex := hex.EncodeToString(crypto.FromECDSA(newPrivKey))

			nonce, err := rpcClient.GetNonce(newAddr.Hex())
			if err != nil {
				fmt.Printf("  ❌ GetNonce: %v\n", err)
				os.Exit(1)
			}
			inputData, _ := accountABI.Pack("setBlsPublicKey", blsPubKey)
			tx := e_types.NewTransaction(nonce, accountContract, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
			signedTx, _ := e_types.SignTx(tx, signer, newPrivKey)
			rawTxBytes, _ := signedTx.MarshalBinary()
			txHash, err := rpcClient.SendRawTransaction("0x" + hex.EncodeToString(rawTxBytes))
			if err != nil {
				fmt.Printf("  ❌ setBlsPublicKey (idx=%d): %v\n", idx, err)
				os.Exit(1)
			}

			// Step 4: confirmAccountWithoutSign (self confirm)
			_, receipt2 := sendTxAndWaitHTTP(
				rpcClient, newPrivKey, newAddr, accountContract,
				accountABI, signer,
				"confirmAccountWithoutSign", newAddr,
			)

			mu.Lock()
			results = append(results, KeyGenResult{
				Index:         idx,
				Address:       newAddr.Hex(),
				PrivateKey:    newPrivKeyHex,
				BlsPubKey:     serverBlsPubKey,
				BlsPrivateKey: "", // private key được node giữ
			})
			completedCount++
			if receipt2 != nil {
				fmt.Printf("  ✅ [%d/%d] Account %s registered & confirmed! (tx: %s)\n", completedCount, count, newAddr.Hex(), txHash)
			} else {
				fmt.Printf("  ⚠️ [%d/%d] Account %s registered but confirm receipt missing (tx: %s)\n", completedCount, count, newAddr.Hex(), txHash)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	fmt.Println("\n  ✅ BLS Registration HTTP flow completed!")
	if len(results) > 0 {
		outBytes, err := json.MarshalIndent(results, "", "  ")
		if err == nil {
			err = os.WriteFile(outFile, outBytes, 0644)
			if err == nil {
				fmt.Printf("  💾 Đã lưu thành công %d keys vào file: %s\n", len(results), outFile)
			} else {
				fmt.Printf("  ❌ Không thể lưu file %s: %v\n", outFile, err)
			}
		}
	}
}

func registerExistingWalletBLSHTTP(
	rpcClient *rpc.RPCClient,
	accountABI abi.ABI,
	accountContract common.Address,
	adminPrivKey *ecdsa.PrivateKey,
	adminAddr common.Address,
	walletPrivKey *ecdsa.PrivateKey,
	walletAddr common.Address,
	signer e_types.Signer,
	noConfirm bool,
) {
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Printf("║  TEST: Register BLS for existing wallet (HTTP)        ║\n")
	fmt.Printf("║  Wallet: %s\n", walletAddr.Hex())
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	// Step 2: Gửi getPublickeyBls qua RpcEthCall để lấy BLS pubkey từ node
	getPubKeyInput, _ := accountABI.Pack("getPublickeyBls")
	blsPubKeyResult, err := rpcClient.EthCall(accountContract.Hex(), "0x"+hex.EncodeToString(getPubKeyInput))
	if err != nil {
		fmt.Printf("  ❌ getPublickeyBls RpcEthCall: %v\n", err)
		return
	}

	var blsPubKey []byte
	strRes := string(blsPubKeyResult)
	strRes = strings.Trim(strRes, "\"")
	strRes = strings.TrimPrefix(strRes, "0x")
	decoded, errHex := hex.DecodeString(strRes)
	if errHex == nil && len(decoded) >= 48 {
		blsPubKey = decoded
	} else {
		err = accountABI.UnpackIntoInterface(&blsPubKey, "getPublickeyBls", blsPubKeyResult)
		if err != nil {
			fmt.Printf("  ❌ Decode getPublickeyBls: %v\n", err)
			return
		}
	}

	serverBlsPubKey := "0x" + hex.EncodeToString(blsPubKey)
	fmt.Printf("  ✅ Fetched BLS PublicKey from node: %s (%d bytes)\n", serverBlsPubKey, len(blsPubKey))

	if len(blsPubKey) == 0 {
		fmt.Println("  ❌ BLS pubkey rỗng, node có vẻ chưa tạo key cho ví này!")
		return
	}

	// Step 3: Gửi setBlsPublicKey
	nonce, _ := rpcClient.GetNonce(walletAddr.Hex())
	inputData, _ := accountABI.Pack("setBlsPublicKey", blsPubKey)
	tx := e_types.NewTransaction(nonce, accountContract, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, walletPrivKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	txHash, err := rpcClient.SendRawTransaction("0x" + hex.EncodeToString(rawTxBytes))
	if err != nil {
		fmt.Printf("  ❌ setBlsPublicKey: %v\n", err)
		return
	}
	fmt.Printf("  ✅ setBlsPublicKey sent: txHash=%s\n", txHash)

	// Đợi receipt bị bỏ qua để chạy nhanh, dùng getNonce (pending) cho tx sau

	if !noConfirm {
		// Step 4: confirmAccountWithoutSign (self confirm)
		fmt.Printf("  ℹ️  Account %s self-confirming...\n", walletAddr.Hex())
		_, receipt2 := sendTxAndWaitHTTP(
			rpcClient, walletPrivKey, walletAddr, accountContract,
			accountABI, signer,
			"confirmAccountWithoutSign", walletAddr,
		)
		if receipt2 != nil {
			// fmt.Printf("  ✅ Receipt (poll HTTP): status=%s, gasUsed=%s\n", receipt2.Status, receipt2.GasUsed)
		} else {
			fmt.Println("  ⚠️ confirmAccountWithoutSign — receipt not available")
		}
	} else {
		fmt.Println("  ⏭️  Skipped confirmAccountWithoutSign (--no-confirm=true)")
	}

	fmt.Println("\n  ✅ BLS Registration HTTP for existing wallet completed!")
}
