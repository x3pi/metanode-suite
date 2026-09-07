package main

import (
	"testing"
)

func TestContractCreatorInfo(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestContractCreatorInfo failed: %v", err)
	}
}
