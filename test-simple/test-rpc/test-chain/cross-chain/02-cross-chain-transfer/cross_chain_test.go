package main

import (
	"math/big"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

// TestCrossChain_ExecClustersOnline checks that both Execution Clusters are online and responding.
func TestCrossChain_ExecClustersOnline(t *testing.T) {
	cfg := common.LoadClusterConfig()

	b1, err1 := common.GetBlockNumber(cfg.Exec1URL)
	if err1 != nil {
		t.Fatalf("Exec Cluster 1 (%s) offline or unreachable: %v", cfg.Exec1URL, err1)
	}
	t.Logf("✅ Exec Cluster 1 (%s) Online. Current block: %d", cfg.Exec1URL, b1)

	b2, err2 := common.GetBlockNumber(cfg.Exec2URL)
	if err2 != nil {
		t.Fatalf("Exec Cluster 2 (%s) offline or unreachable: %v", cfg.Exec2URL, err2)
	}
	t.Logf("✅ Exec Cluster 2 (%s) Online. Current block: %d", cfg.Exec2URL, b2)
}

// TestCrossChain_Transfer_Exec1ToExec2 sends a cross-chain transfer from Exec 1 to Exec 2.
func TestCrossChain_Transfer_Exec1ToExec2(t *testing.T) {
	cfg := common.LoadClusterConfig()
	targetAddr := ethcommon.HexToAddress(cfg.Exec2Address)
	if targetAddr == (ethcommon.Address{}) {
		targetAddr = common.RandomAddress()
	}
	amount := big.NewInt(1000000000000000) // 10^15 wei (0.001 token)

	initBal, err := common.GetAccountBalance(cfg.Exec2URL, targetAddr)
	if err != nil {
		t.Logf("Warning querying initial balance: %v (defaulting to 0)", err)
		initBal = big.NewInt(0)
	}

	txHash, err := common.SendCrossChainTransfer(cfg.Exec1URL, targetAddr, amount, cfg.GetExec1SenderKey())
	if err != nil {
		t.Fatalf("Failed to send cross chain transfer Exec1 -> Exec2: %v", err)
	}
	t.Logf("✅ Cross-chain transfer Exec1 -> Exec2 submitted. TxHash: %s", txHash)

	// Poll for cross-chain relay and destination balance update
	credited := false
	maxWaitSec := 60
	var finalBal *big.Int
	for i := 0; i < maxWaitSec; i++ {
		time.Sleep(1 * time.Second)
		newBal, errBal := common.GetAccountBalance(cfg.Exec2URL, targetAddr)
		if errBal == nil && newBal != nil {
			finalBal = newBal
			if newBal.Cmp(initBal) > 0 {
				diff := new(big.Int).Sub(newBal, initBal)
				t.Logf("✅ Exec 2 destination balance increased after %ds! (+%s wei, new balance: %s)", i+1, diff.String(), newBal.String())
				credited = true
				break
			}
		}
	}
	if !credited {
		lastStr := "nil"
		if finalBal != nil {
			lastStr = finalBal.String()
		}
		t.Fatalf("Destination account %s on Exec 2 did NOT receive funds after %ds (initial: %s, final: %s)", targetAddr.Hex(), maxWaitSec, initBal.String(), lastStr)
	}
}

// TestCrossChain_Transfer_Exec2ToExec1 sends a cross-chain transfer in the reverse direction.
func TestCrossChain_Transfer_Exec2ToExec1(t *testing.T) {
	cfg := common.LoadClusterConfig()
	targetAddr := ethcommon.HexToAddress(cfg.Exec1Address)
	if targetAddr == (ethcommon.Address{}) {
		targetAddr = common.RandomAddress()
	}
	amount := big.NewInt(1000000000000000) // 10^15 wei (0.001 token)

	initBal, err := common.GetAccountBalance(cfg.Exec1URL, targetAddr)
	if err != nil {
		t.Logf("Warning querying initial balance: %v (defaulting to 0)", err)
		initBal = big.NewInt(0)
	}

	txHash, err := common.SendCrossChainTransfer(cfg.Exec2URL, targetAddr, amount, cfg.GetExec2SenderKey())
	if err != nil {
		t.Fatalf("Failed to send cross chain transfer Exec2 -> Exec1: %v", err)
	}
	t.Logf("✅ Reverse cross-chain transfer Exec2 -> Exec1 submitted. TxHash: %s", txHash)

	// Poll for cross-chain relay and destination balance update
	credited := false
	maxWaitSec := 60
	var finalBal *big.Int
	for i := 0; i < maxWaitSec; i++ {
		time.Sleep(1 * time.Second)
		newBal, errBal := common.GetAccountBalance(cfg.Exec1URL, targetAddr)
		if errBal == nil && newBal != nil {
			finalBal = newBal
			if newBal.Cmp(initBal) > 0 {
				diff := new(big.Int).Sub(newBal, initBal)
				t.Logf("✅ Exec 1 destination balance increased after %ds! (+%s wei, new balance: %s)", i+1, diff.String(), newBal.String())
				credited = true
				break
			}
		}
	}
	if !credited {
		lastStr := "nil"
		if finalBal != nil {
			lastStr = finalBal.String()
		}
		t.Fatalf("Destination account %s on Exec 1 did NOT receive funds after %ds (initial: %s, final: %s)", targetAddr.Hex(), maxWaitSec, initBal.String(), lastStr)
	}
}

// TestCrossChain_ConservationBalance queries the conservation check on both execution clusters.
func TestCrossChain_ConservationBalance(t *testing.T) {
	cfg := common.LoadClusterConfig()

	cons1, err1 := common.GetConservation(cfg.Exec1URL)
	if err1 != nil {
		t.Logf("Exec 1 conservation endpoint info: %v", err1)
	} else {
		t.Logf("✅ Exec 1 Conservation State: %+v", cons1)
	}

	cons2, err2 := common.GetConservation(cfg.Exec2URL)
	if err2 != nil {
		t.Logf("Exec 2 conservation endpoint info: %v", err2)
	} else {
		t.Logf("✅ Exec 2 Conservation State: %+v", cons2)
	}
}
