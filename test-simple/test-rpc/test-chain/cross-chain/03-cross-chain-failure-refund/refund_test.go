package main

import (
	"testing"
)

func TestCrossChainFailureRefund(t *testing.T) {
	// Chạy kiểm thử failure refund giữa chain 101 -> 102
	err := RunRefundTest("101", "102", "")
	if err != nil {
		t.Fatalf("❌ Test Cross-Chain Failure Refund failed: %v", err)
	}
}
