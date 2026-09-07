package main

import (
	"testing"
)

func TestReadWrite(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
