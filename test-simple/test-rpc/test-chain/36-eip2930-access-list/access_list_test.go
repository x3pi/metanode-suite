package main

import (
	"testing"
)

func TestEIP2930AccessList(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP2930AccessList failed: %v", err)
	}
}
