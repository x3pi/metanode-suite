package main

import (
	"testing"
)

func TestNativeManyToOne(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
