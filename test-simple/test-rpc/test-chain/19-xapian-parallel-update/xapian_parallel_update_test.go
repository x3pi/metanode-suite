package main

import (
	"testing"
)

func TestXapianParallelUpdate(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestXapianParallelUpdate failed: %v", err)
	}
}
