package main

import (
	"testing"
)

func TestEIP4844EdgeCases(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP4844EdgeCases failed: %v", err)
	}
}
