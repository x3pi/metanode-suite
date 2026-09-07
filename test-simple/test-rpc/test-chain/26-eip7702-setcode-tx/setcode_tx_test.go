package main

import (
	"testing"
)

func TestEIP7702SetCodeTx(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP7702SetCodeTx failed: %v", err)
	}
}
