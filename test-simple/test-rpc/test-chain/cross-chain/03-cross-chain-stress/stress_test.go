package main

import (
	"math/big"
	"sync"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

func TestCrossChain_ConcurrentTransfers(t *testing.T) {
	cfg := common.LoadClusterConfig()

	// Pre-check cluster availability
	_, err := common.GetBlockNumber(cfg.Exec1URL)
	if err != nil {
		t.Skipf("Exec 1 unreachable (%v), skipping stress test", err)
	}

	const count = 5
	var wg sync.WaitGroup
	errCh := make(chan error, count)

	targetAddr := ethcommon.HexToAddress(cfg.Exec2Address)
	if targetAddr == (ethcommon.Address{}) {
		targetAddr = common.RandomAddress()
	}

	t.Logf("Spawning %d concurrent cross-chain transfers Exec 1 -> Exec 2...", count)
	start := time.Now()

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			senderKey := cfg.GetStressSenderKey(idx)
			amount := big.NewInt(1000000000000) // 10^12 wei
			_, err := common.SendCrossChainTransfer(cfg.Exec1URL, targetAddr, amount, senderKey)
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	elapsed := time.Since(start)
	t.Logf("Finished %d concurrent transfers in %v", count, elapsed)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Fatalf("%d out of %d concurrent transfers failed. First error: %v", len(errs), count, errs[0])
	}
}
