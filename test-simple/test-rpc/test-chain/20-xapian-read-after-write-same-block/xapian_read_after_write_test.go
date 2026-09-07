package main

import (
	"testing"
)

func TestXapianReadAfterWriteSameBlock(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestXapianReadAfterWriteSameBlock failed: %v", err)
	}
}
