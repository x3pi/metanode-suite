package main

import (
	"testing"
)

func TestNativeOneToMany(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
