package main

import (
	"testing"
)

func TestXapianEvmContract(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
