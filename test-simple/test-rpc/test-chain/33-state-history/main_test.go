package main

import (
	"testing"
)

func TestStateHistory(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
