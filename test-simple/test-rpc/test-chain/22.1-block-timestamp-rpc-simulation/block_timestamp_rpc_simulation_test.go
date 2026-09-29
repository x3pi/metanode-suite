package main

import "testing"

func TestBlockTimestampRPCSimulation(t *testing.T) {
	if err := RunTest(""); err != nil {
		t.Fatal(err)
	}
}
