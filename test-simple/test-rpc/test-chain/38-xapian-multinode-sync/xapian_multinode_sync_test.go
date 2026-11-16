package main

import (
	"testing"
)

func TestXapianMultiNodeSync(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatalf("38-xapian-multinode-sync failed: %v", err)
	}
}
