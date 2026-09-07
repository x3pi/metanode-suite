package main

import (
	"testing"
)

func TestXapianParallelReadWrite(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
