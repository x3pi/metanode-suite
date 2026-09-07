package main

import (
	"testing"
)

func TestMixedAllTypesBlock(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestMixedAllTypesBlock failed: %v", err)
	}
}
