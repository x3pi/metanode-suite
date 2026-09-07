package main

import (
	"testing"
)

func TestAMMDex(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
