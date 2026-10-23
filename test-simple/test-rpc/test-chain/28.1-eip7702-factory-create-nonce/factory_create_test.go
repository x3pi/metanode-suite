package main

import (
	"testing"
)

func TestEIP7702FactoryCreateNonce(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP7702FactoryCreateNonce failed: %v", err)
	}
}
