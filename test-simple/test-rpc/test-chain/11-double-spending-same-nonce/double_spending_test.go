package main

import (
	"testing"
)

func TestDoubleSpendingSameNonce(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
