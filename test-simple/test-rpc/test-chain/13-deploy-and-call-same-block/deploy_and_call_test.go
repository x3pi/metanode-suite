package main

import (
	"testing"
)

func TestDeployAndCallSameBlock(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
