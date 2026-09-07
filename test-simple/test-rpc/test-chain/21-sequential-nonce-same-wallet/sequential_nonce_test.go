package main

import (
	"testing"
)

func TestSequentialNonceSameWallet(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestSequentialNonceSameWallet failed: %v", err)
	}
}
