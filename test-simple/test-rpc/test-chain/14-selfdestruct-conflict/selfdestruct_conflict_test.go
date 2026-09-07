package main

import (
	"testing"
)

func TestSelfdestructConflict(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
