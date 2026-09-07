package main

import (
	"testing"
)

func TestBlockhashOpcodeVerifier(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestBlockhashOpcodeVerifier failed: %v", err)
	}
}
