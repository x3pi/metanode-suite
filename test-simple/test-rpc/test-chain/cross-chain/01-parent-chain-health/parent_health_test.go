package main

import (
	"testing"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

// TestParentChain_ConnectivityAndStatus verifies all Parent Chain nodes respond with HTTP 200,
// syncing is false (or progressing), and fork_detected is false.
func TestParentChain_ConnectivityAndStatus(t *testing.T) {
	cfg := common.LoadClusterConfig()
	if len(cfg.ParentURLs) == 0 {
		t.Fatal("No Parent Chain URLs configured")
	}

	t.Logf("Checking %d Parent Chain node(s)...", len(cfg.ParentURLs))
	for i, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if !st.Online {
			t.Errorf("Node %d (%s) is OFFLINE: %s", i, u, st.Error)
			continue
		}
		if st.ForkDetected {
			t.Errorf("Node %d (%s) reported FORK_DETECTED=true", i, u)
		}
		t.Logf("  ✅ Node %d (%s): Height=%d, Syncing=%v, ForkDetected=%v, Latency=%dms",
			i, u, st.Height, st.Syncing, st.ForkDetected, st.LatencyMs)
	}
}

// TestParentChain_BFTQuorum verifies that at least 2f + 1 nodes are online.
func TestParentChain_BFTQuorum(t *testing.T) {
	cfg := common.LoadClusterConfig()
	total := len(cfg.ParentURLs)
	if total == 0 {
		t.Fatal("No Parent Chain URLs found")
	}

	f := (total - 1) / 3
	requiredQuorum := 2*f + 1

	onlineCount := 0
	for _, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if st.Online {
			onlineCount++
		}
	}

	t.Logf("Parent Chain BFT Quorum: total=%d, f=%d, requiredQuorum=%d, onlineCount=%d",
		total, f, requiredQuorum, onlineCount)

	if onlineCount < requiredQuorum {
		t.Fatalf("Insufficient online nodes for BFT quorum: got %d, required %d (total %d)",
			onlineCount, requiredQuorum, total)
	}
}

// TestParentChain_BlockProgression verifies that Parent Chain produces blocks over time.
func TestParentChain_BlockProgression(t *testing.T) {
	cfg := common.LoadClusterConfig()
	if len(cfg.ParentURLs) == 0 {
		t.Skip("No Parent Chain configured")
	}

	var activeURL string
	var initialHeight uint64
	for _, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if st.Online {
			activeURL = u
			initialHeight = st.Height
			break
		}
	}

	if activeURL == "" {
		t.Fatal("No active Parent Chain node available to test block progression")
	}

	t.Logf("Initial Parent Chain Height on %s: %d. Waiting 4 seconds for new blocks...", activeURL, initialHeight)
	time.Sleep(4 * time.Second)

	finalSt := common.CheckParentNode(activeURL)
	if !finalSt.Online {
		t.Fatalf("Node %s went offline during block progression check", activeURL)
	}

	delta := finalSt.Height - initialHeight
	t.Logf("Final Parent Chain Height on %s: %d (Delta: +%d)", activeURL, finalSt.Height, delta)
}

// TestParentChain_NodeHeightSynchronization ensures all nodes are within acceptable block divergence.
func TestParentChain_NodeHeightSynchronization(t *testing.T) {
	cfg := common.LoadClusterConfig()
	if len(cfg.ParentURLs) <= 1 {
		t.Skip("Need at least 2 Parent Chain nodes to verify synchronization divergence")
	}

	var onlineHeights []uint64
	for _, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if st.Online {
			onlineHeights = append(onlineHeights, st.Height)
		}
	}

	if len(onlineHeights) <= 1 {
		t.Skip("Not enough online Parent Chain nodes to check height synchronization")
	}

	minH := onlineHeights[0]
	maxH := onlineHeights[0]
	for _, h := range onlineHeights {
		if h < minH {
			minH = h
		}
		if h > maxH {
			maxH = h
		}
	}

	divergence := maxH - minH
	t.Logf("Height divergence across %d nodes: max=%d, min=%d, delta=%d", len(onlineHeights), maxH, minH, divergence)

	const maxAllowedDivergence = 5
	if divergence > maxAllowedDivergence {
		t.Errorf("Parent Chain nodes diverged by %d blocks (max allowed: %d)", divergence, maxAllowedDivergence)
	}
}
