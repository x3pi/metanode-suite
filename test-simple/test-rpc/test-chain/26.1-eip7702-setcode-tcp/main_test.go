package main

import (
	"testing"
)

func TestEIP7702SetCodeTCP(t *testing.T) {
	if err := RunTest("../config.json", "data.json"); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
