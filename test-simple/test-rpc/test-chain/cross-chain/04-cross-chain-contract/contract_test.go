package main

import (
	"math/big"
	"testing"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

func TestCrossChain_ContractWorkflow(t *testing.T) {
	cfg := common.LoadClusterConfig()

	// 1. Check clusters online
	_, err1 := common.GetBlockNumber(cfg.Exec1URL)
	_, err2 := common.GetBlockNumber(cfg.Exec2URL)
	if err1 != nil || err2 != nil {
		t.Skipf("Clusters not online (err1=%v, err2=%v), skipping contract test", err1, err2)
	}

	counterInfo, ok := cfg.Contracts["TestCounter"]
	if !ok || counterInfo.Bytecode == "" {
		t.Fatalf("TestCounter bytecode not found in config")
	}

	// 2. Deploy TestCounter on Exec 2
	deployerKey := cfg.GetExec2SenderKey()
	contractAddr, deployTx, errDeploy := common.DeployContract(cfg.Exec2URL, deployerKey, counterInfo.Bytecode)
	if errDeploy != nil {
		t.Fatalf("Failed to deploy TestCounter on Exec 2: %v", errDeploy)
	}
	t.Logf("✅ Contract deployed on Exec 2: %s (TxHash: %s)", contractAddr.Hex(), deployTx.Hex())

	time.Sleep(2 * time.Second)

	// 3. Trigger cross-chain transfer Exec 1 -> Contract on Exec 2
	amount := big.NewInt(10000000000000)
	crossTx, errCross := common.SendCrossChainTransfer(cfg.Exec1URL, contractAddr, amount, cfg.GetExec1SenderKey(), cfg.Exec2BLSPubkey)
	if errCross != nil {
		t.Fatalf("Failed to trigger cross-chain transfer to contract: %v", errCross)
	}
	t.Logf("✅ Cross-chain transfer to contract submitted: %s", crossTx)

	time.Sleep(2 * time.Second)

	// 4. Invoke increment() on contract
	incTx, errInc := common.CallContract(cfg.Exec2URL, deployerKey, contractAddr, incrementSelector)
	if errInc != nil {
		t.Fatalf("Failed to call increment() on contract: %v", errInc)
	}
	t.Logf("✅ increment() invoked on contract: %s", incTx.Hex())
}
