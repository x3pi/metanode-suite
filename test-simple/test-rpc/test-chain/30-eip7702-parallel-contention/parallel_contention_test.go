package main

import (
	"testing"
)

func TestEIP7702ParallelContention(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP7702ParallelContention failed: %v", err)
	}
}
