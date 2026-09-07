package main

import (
	"testing"
)

func TestEIP7702DelegatedExecutionAndRevocation(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP7702DelegatedExecutionAndRevocation failed: %v", err)
	}
}
