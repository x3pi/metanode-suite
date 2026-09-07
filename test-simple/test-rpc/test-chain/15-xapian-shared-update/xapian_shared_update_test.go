package main

import (
	"testing"
)

func TestXapianSharedUpdate(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
