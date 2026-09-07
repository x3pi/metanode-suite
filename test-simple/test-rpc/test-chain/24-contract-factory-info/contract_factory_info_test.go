package main

import (
	"testing"
)

func TestContractFactoryInfo(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestContractFactoryInfo failed: %v", err)
	}
}
