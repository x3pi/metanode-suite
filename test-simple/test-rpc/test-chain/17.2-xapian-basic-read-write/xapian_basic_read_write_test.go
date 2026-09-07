package main

import (
	"testing"
)

func TestXapianBasicReadWrite(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
