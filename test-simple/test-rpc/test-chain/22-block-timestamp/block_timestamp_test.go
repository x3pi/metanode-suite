package main

import (
	"testing"
)

func TestBlockTimestamp(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestBlockTimestamp failed: %v", err)
	}
}
