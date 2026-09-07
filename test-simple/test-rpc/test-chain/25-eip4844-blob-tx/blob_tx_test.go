package main

import (
	"testing"
)

func TestEIP4844BlobTx(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestEIP4844BlobTx failed: %v", err)
	}
}
