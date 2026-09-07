package main

import (
	"testing"
)

func TestUpdateDifferentVariables(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("❌ %v", err)
	}
}
