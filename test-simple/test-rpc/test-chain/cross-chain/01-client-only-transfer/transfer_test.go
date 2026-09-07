package main

import (
	"testing"
)

func TestCrossChainTransferAndCall(t *testing.T) {
	// Chạy với chain 101 -> 102 mặc định
	err := RunTransferTest("101", "102", 500.0, "", "", "", "", "")
	if err != nil {
		t.Fatalf("❌ Test Cross-Chain Transfer failed: %v", err)
	}
}
