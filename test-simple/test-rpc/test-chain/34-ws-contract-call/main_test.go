package main

import (
	"testing"
)

func TestWsContractCall(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
