package main

import (
	"testing"
)

func TestHttpsContractCall(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}

