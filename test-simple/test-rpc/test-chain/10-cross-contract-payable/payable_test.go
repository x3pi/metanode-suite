package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestCrossContractPayable(t *testing.T) {
	cfg, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		t.Fatalf("❌ Lỗi kết nối RPC (%s): %v", cfg.RPCUrl, err)
	}

	if len(cfg.PrivateKeys) < 4 {
		t.Fatalf("❌ Cần ít nhất 4 private keys để test, tìm thấy: %d", len(cfg.PrivateKeys))
	}

	// 1. Load ABI & Bytecode
	parsedTargetABI, err := abi.JSON(strings.NewReader(cfg.Contracts["PayableTargetContract"].ABI))
	if err != nil {
		t.Fatalf("ABI parse PayableTargetContract err: %v", err)
	}
	bytecodeTarget, err := hexutil.Decode("0x" + cfg.Contracts["PayableTargetContract"].Bytecode)
	if err != nil {
		t.Fatalf("Bytecode PayableTargetContract err: %v", err)
	}

	parsedCallerABI, err := abi.JSON(strings.NewReader(cfg.Contracts["PayableCallerContract"].ABI))
	if err != nil {
		t.Fatalf("ABI parse PayableCallerContract err: %v", err)
	}
	bytecodeCaller, err := hexutil.Decode("0x" + cfg.Contracts["PayableCallerContract"].Bytecode)
	if err != nil {
		t.Fatalf("Bytecode PayableCallerContract err: %v", err)
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		t.Fatalf("Invalid private key[0]: %v", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// 2. Deploy PayableTargetContract
	t.Log("🚀 Deploying PayableTargetContract...")
	targetAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeTarget, nil)
	if err != nil {
		t.Fatalf("❌ Deploy PayableTargetContract thất bại: %v", err)
	}
	t.Logf("📌 PayableTargetContract deployed at: %s", targetAddr.Hex())

	// 3. Deploy PayableCallerContract(targetAddr)
	t.Log("🚀 Deploying PayableCallerContract...")
	constructorData, err := parsedCallerABI.Pack("", *targetAddr)
	if err != nil {
		t.Fatalf("❌ Pack constructor CallerContract thất bại: %v", err)
	}
	bytecodeCallerWithArgs := append(bytecodeCaller, constructorData...)

	callerAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeCallerWithArgs, nil)
	if err != nil {
		t.Fatalf("❌ Deploy CallerContract thất bại: %v", err)
	}
	t.Logf("📌 PayableCallerContract deployed at: %s", callerAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	numTxs := len(cfg.PrivateKeys) - 1
	txHashes := make([]common.Hash, numTxs)

	t.Logf("🔥 Bắt đầu test Cross-Contract Payable: %d ví cùng gọi PayableCallerContract.callTarget()...", numTxs)
	start := time.Now()

	for i := 1; i <= numTxs; i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d invalid pk: %v", idx, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d nonce error: %v", idx, err))
				errsMu.Unlock()
				return
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil {
				gasPrice = big.NewInt(1000000000)
			}

			callValue := big.NewInt(int64(idx * 100))
			data, _ := parsedCallerABI.Pack("callTarget")
			gasLimit := uint64(200000)

			tx := types.NewTransaction(nonce, *callerAddr, callValue, gasLimit, gasPrice, data)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d sign tx error: %v", idx, err))
				errsMu.Unlock()
				return
			}

			t.Logf("📤 Wallet %d (%s) gửi tx: Hash=%s | Value=%s wei", idx, from.Hex()[:10]+"...", signedTx.Hash().Hex(), callValue.String())

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d send tx error: %v", idx, err))
				errsMu.Unlock()
				return
			}
			txHashes[idx-1] = signedTx.Hash()
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()

	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("❌ Lỗi gửi tx: %v", e)
		}
		t.FailNow()
	}

	t.Log("⏳ Chờ các giao dịch được confirm trong block trên chain...")
	successCount := 0
	revertCount := 0
	for i := 0; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 60*time.Second {
				t.Fatalf("❌ Timeout waiting for receipt of tx %s", hash.Hex())
			}
			receipt, err := client.TransactionReceipt(context.Background(), hash)
			if err != nil && !strings.Contains(err.Error(), "not found") {
				t.Fatalf("Lỗi kết nối RPC: %v", err)
			}
			if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if receipt.Status != 1 {
					t.Errorf("❌ Tx %s bị revert trên chain!", hash.Hex())
					revertCount++
				} else {
					t.Logf("✅ Tx Confirmed trên Chain: Hash=%s | Block=#%d | GasUsed=%d | Status=SUCCESS (1)",
						hash.Hex(), receipt.BlockNumber.Uint64(), receipt.GasUsed)
					successCount++
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	t.Logf("📊 KẾT QUẢ: Gửi & chờ trong %v (Thành công: %d, Revert: %d)", elapsed, successCount, revertCount)

	// Truy vấn xác thực mẫu trực tiếp từ RPC node
	if len(txHashes) > 0 && txHashes[0] != (common.Hash{}) {
		sampleHash := txHashes[0]
		txData, isPending, err := client.TransactionByHash(context.Background(), sampleHash)
		if err == nil && txData != nil {
			rec, _ := client.TransactionReceipt(context.Background(), sampleHash)
			t.Log("═══════════════════════════════════════════════════════════")
			t.Log("🔍 XÁC THỰC DỮ LIỆU THỰC TẾ TRÊN BLOCKCHAIN LEDGER (VIA RPC):")
			t.Logf("   • Tx Hash:      %s", txData.Hash().Hex())
			t.Logf("   • Is Pending:   %v (Đã lưu vĩnh viễn vào Block)", isPending)
			if rec != nil {
				t.Logf("   • Block Number: #%d", rec.BlockNumber.Uint64())
				t.Logf("   • Block Hash:   %s", rec.BlockHash.Hex())
				t.Logf("   • Gas Used:     %d", rec.GasUsed)
			}
			t.Logf("   • Value:        %s wei", txData.Value().String())
			if txData.To() != nil {
				t.Logf("   • To Contract:  %s", txData.To().Hex())
			}
			t.Log("═══════════════════════════════════════════════════════════")
		}
	}

	// Assertions
	dataValue, _ := parsedTargetABI.Pack("value")
	msgValue := ethereum.CallMsg{To: targetAddr, Data: dataValue}
	resValueBytes, err := client.CallContract(context.Background(), msgValue, nil)
	if err != nil {
		t.Fatalf("Call value err: %v", err)
	}
	unpackedValue, _ := parsedTargetABI.Unpack("value", resValueBytes)
	finalValue := unpackedValue[0].(*big.Int)

	dataEth, _ := parsedTargetABI.Pack("totalEthReceived")
	msgEth := ethereum.CallMsg{To: targetAddr, Data: dataEth}
	resEthBytes, err := client.CallContract(context.Background(), msgEth, nil)
	if err != nil {
		t.Fatalf("Call totalEthReceived err: %v", err)
	}
	unpackedEth, _ := parsedTargetABI.Unpack("totalEthReceived", resEthBytes)
	finalTotalEth := unpackedEth[0].(*big.Int)

	expectedEthSum := int64(0)
	for i := 1; i <= numTxs; i++ {
		expectedEthSum += int64(i * 100)
	}

	actualBalance, err := client.BalanceAt(context.Background(), *targetAddr, nil)
	if err != nil {
		t.Fatalf("BalanceAt err: %v", err)
	}

	t.Logf("🔍 PayableTargetContract.value = %s (Kỳ vọng: %d)", finalValue.String(), numTxs)
	t.Logf("🔍 PayableTargetContract.totalEthReceived = %s (Kỳ vọng: %d)", finalTotalEth.String(), expectedEthSum)
	t.Logf("🔍 PayableTargetContract.balance = %s wei (Kỳ vọng: %d wei)", actualBalance.String(), expectedEthSum)

	if successCount != numTxs {
		t.Errorf("❌ Số tx thành công (%d) != kỳ vọng (%d)", successCount, numTxs)
	}
	if finalValue.Int64() != int64(numTxs) {
		t.Errorf("❌ Final value (%d) != kỳ vọng (%d)", finalValue.Int64(), numTxs)
	}
	if finalTotalEth.Int64() != expectedEthSum {
		t.Errorf("❌ Total ETH (%d) != kỳ vọng (%d)", finalTotalEth.Int64(), expectedEthSum)
	}
	if actualBalance.Int64() != expectedEthSum {
		t.Errorf("❌ Balance (%d) != kỳ vọng (%d)", actualBalance.Int64(), expectedEthSum)
	}
}
