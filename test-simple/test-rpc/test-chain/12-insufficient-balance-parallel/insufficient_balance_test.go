package main

import (
	"testing"
)

func TestInsufficientBalanceParallel(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
