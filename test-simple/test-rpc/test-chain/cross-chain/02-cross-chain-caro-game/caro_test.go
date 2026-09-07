package main

import (
	"testing"
)

func TestCrossChainCaroGame(t *testing.T) {
	// Chạy trận đấu cờ caro tự động (interactive=false, scripted moves X thắng chéo)
	err := RunCaroTest("101", "102", "", false)
	if err != nil {
		t.Fatalf("❌ Test Cross-Chain Caro Game failed: %v", err)
	}
}
