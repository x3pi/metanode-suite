package main

import (
	"testing"
)

func TestXapianParallelStress(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("TestXapianParallelStress failed: %v", err)
	}
}
