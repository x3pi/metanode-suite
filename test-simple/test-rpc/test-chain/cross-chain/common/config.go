package common

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// ContractInfo stores ABI and Bytecode of a smart contract
type ContractInfo struct {
	ABI      string `json:"abi"`
	Bytecode string `json:"bytecode"`
}

// ClusterConfig holds discovered endpoints for Parent Chain and Execution Shards
type ClusterConfig struct {
	ParentURLs              []string
	Exec1URL                string
	Exec2URL                string
	ConfigFile              string
	Exec1ChainID            uint64
	Exec2ChainID            uint64
	Exec1Address            string
	Exec2Address            string
	Exec1BLSPubkey          string
	Exec2BLSPubkey          string
	Exec1PrivateKeys        []string
	Exec2PrivateKeys        []string
	PrivateKeys             []string
	GatewayContract         string
	ParentConnectionAddress string
	Contracts               map[string]ContractInfo
}

// ParentNodeStatus stores health info of a Parent Chain node
type ParentNodeStatus struct {
	Name         string `json:"name,omitempty"`
	URL          string `json:"url"`
	Height       uint64 `json:"height"`
	Syncing      bool   `json:"syncing"`
	ForkDetected bool   `json:"fork_detected"`
	StateRoot    string `json:"state_root"`
	ChainID      uint64 `json:"chain_id"`
	LatencyMs    int64  `json:"latency_ms"`
	Online       bool   `json:"online"`
	Error        string `json:"error,omitempty"`
}

// GetExec1SenderKey returns a funded sender private key for Exec 1
func (c *ClusterConfig) GetExec1SenderKey(idx ...int) string {
	i := 0
	if len(idx) > 0 {
		i = idx[0]
	}
	if len(c.Exec1PrivateKeys) > 0 {
		return c.Exec1PrivateKeys[i%len(c.Exec1PrivateKeys)]
	}
	if len(c.PrivateKeys) > 0 {
		return c.PrivateKeys[i%len(c.PrivateKeys)]
	}
	return "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
}

// GetExec2SenderKey returns a funded sender private key for Exec 2
func (c *ClusterConfig) GetExec2SenderKey(idx ...int) string {
	i := 0
	if len(idx) > 0 {
		i = idx[0]
	}
	if len(c.Exec2PrivateKeys) > 0 {
		return c.Exec2PrivateKeys[i%len(c.Exec2PrivateKeys)]
	}
	return "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a"
}

// GetStressSenderKey returns a unique sender private key for stress worker index
func (c *ClusterConfig) GetStressSenderKey(idx int) string {
	if len(c.PrivateKeys) > 0 {
		return c.PrivateKeys[idx%len(c.PrivateKeys)]
	}
	if len(c.Exec1PrivateKeys) > 0 {
		return c.Exec1PrivateKeys[idx%len(c.Exec1PrivateKeys)]
	}
	return c.GetExec1SenderKey(idx)
}

// LoadClusterConfig discovers topology from config.json, /tmp/rpc_nodes.json, environment variables, or defaults
func LoadClusterConfig() *ClusterConfig {
	cfg := &ClusterConfig{
		ParentURLs: []string{
			"http://192.168.1.223:18601",
			"http://192.168.1.223:18602",
			"http://192.168.1.223:18603",
			"http://192.168.1.223:18604",
		},
		Exec1URL:        "http://192.168.1.223:8747",
		Exec2URL:        "http://192.168.1.223:8750",
		Exec1ChainID:    991,
		Exec2ChainID:    991,
		Exec1Address:    "0x1F0ECA432E1B18b140814beF0ce1Ba2b09DE44c5",
		Exec2Address:    "0x0d4CC97b62a149a8fe8DE81262270426A80B0935",
		GatewayContract: "0x0000000000000000000000000000000000001002",
		Contracts:       make(map[string]ContractInfo),
	}

	// 1. Search candidate paths for config.json (Single Source of Truth)
	candidates := []string{}
	if envCfg := strings.TrimSpace(os.Getenv("CONFIG_FILE")); envCfg != "" {
		candidates = append(candidates, envCfg)
	}
	if envTestCfg := strings.TrimSpace(os.Getenv("TEST_CONFIG_FILE")); envTestCfg != "" {
		candidates = append(candidates, envTestCfg)
	}
	if envPath := strings.TrimSpace(os.Getenv("METANODE_CONFIG_PATH")); envPath != "" {
		candidates = append(candidates, envPath)
	}

	candidates = append(candidates,
		"/home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/config.json",
		"../config.json",
		"../../config.json",
		"../../../configs/config.json",
		"../../../../configs/config.json",
		"/home/abc/nhat/consensus-chain/metanode-suite/configs/config.json",
		"configs/config.json",
	)

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}

		type RawChainConfig struct {
			ChainID       uint64            `json:"chain_id"`
			Address       string            `json:"address"`
			BLSPubkey     string            `json:"bls_pubkey"`
			BLSPrivateKey string            `json:"bls_private_key"`
			RpcURL        string            `json:"rpc_url"`
			WsURL         string            `json:"ws_url"`
			PrivateKeys   []string          `json:"private_keys"`
			RpcNodes      map[string]string `json:"rpc_nodes"`
		}

		type RawConfigFile struct {
			RpcURL                  string                    `json:"rpc_url"`
			Rpc0                    string                    `json:"rpc_0"`
			Rpc1                    string                    `json:"rpc_1"`
			Rpc2                    string                    `json:"rpc_2"`
			Rpc3                    string                    `json:"rpc_3"`
			ParentConnectionAddress string                    `json:"parent_connection_address"`
			PrivateChains           map[string]RawChainConfig `json:"private_chains"`
			PrivateKeys             []string                  `json:"private_keys"`
			GatewayPrecompile       struct {
				Address string `json:"address"`
			} `json:"gateway_precompile"`
			Contracts map[string]ContractInfo `json:"contracts"`
		}

		var rawCfg RawConfigFile
		if err := json.Unmarshal(data, &rawCfg); err == nil {
			cfg.ConfigFile = p

			if rawCfg.GatewayPrecompile.Address != "" {
				cfg.GatewayContract = rawCfg.GatewayPrecompile.Address
			}
			if rawCfg.ParentConnectionAddress != "" {
				cfg.ParentConnectionAddress = rawCfg.ParentConnectionAddress
			}
			if len(rawCfg.PrivateKeys) > 0 {
				cfg.PrivateKeys = rawCfg.PrivateKeys
			}
			if len(rawCfg.Contracts) > 0 {
				cfg.Contracts = rawCfg.Contracts
			}

			// Chain A / Exec 1
			for _, key := range []string{"chain_a", "exec1"} {
				if cA, ok := rawCfg.PrivateChains[key]; ok {
					if cA.RpcURL != "" {
						cfg.Exec1URL = cA.RpcURL
					}
					if cA.ChainID != 0 {
						cfg.Exec1ChainID = cA.ChainID
					}
					if cA.Address != "" {
						cfg.Exec1Address = cA.Address
					}
					if cA.BLSPubkey != "" {
						cfg.Exec1BLSPubkey = cA.BLSPubkey
					}
					if len(cA.PrivateKeys) > 0 {
						cfg.Exec1PrivateKeys = cA.PrivateKeys
					}
					break
				}
			}

			// Chain B / Exec 2
			for _, key := range []string{"chain_b", "exec2"} {
				if cB, ok := rawCfg.PrivateChains[key]; ok {
					if cB.RpcURL != "" {
						cfg.Exec2URL = cB.RpcURL
					}
					if cB.ChainID != 0 {
						cfg.Exec2ChainID = cB.ChainID
					}
					if cB.Address != "" {
						cfg.Exec2Address = cB.Address
					}
					if cB.BLSPubkey != "" {
						cfg.Exec2BLSPubkey = cB.BLSPubkey
					}
					if len(cB.PrivateKeys) > 0 {
						cfg.Exec2PrivateKeys = cB.PrivateKeys
					}
					break
				}
			}

			// Infer Parent Chain Node URLs
			if rawCfg.RpcURL != "" {
				host := "192.168.1.223"
				if u, err := url.Parse(rawCfg.RpcURL); err == nil && u.Hostname() != "" {
					host = u.Hostname()
				}
				cfg.ParentURLs = []string{
					fmt.Sprintf("http://%s:18601", host),
					fmt.Sprintf("http://%s:18602", host),
					fmt.Sprintf("http://%s:18603", host),
					fmt.Sprintf("http://%s:18604", host),
				}
			}
			break
		}
	}

	// 2. Discover detailed node topology from /tmp/rpc_nodes.json (if present)
	rpcNodesPath := os.Getenv("RPC_NODES_FILE")
	if rpcNodesPath == "" {
		rpcNodesPath = os.Getenv("RPC_NODES_JSON_PATH")
	}
	if rpcNodesPath == "" {
		rpcNodesPath = "/tmp/rpc_nodes.json"
	}

	if data, err := os.ReadFile(rpcNodesPath); err == nil && len(data) > 0 {
		var topology struct {
			RootAnchor  string `json:"root_anchor"`
			ParentNodes map[string]struct {
				RpcURL string `json:"rpc_url"`
			} `json:"parent_nodes"`
			Nodes         map[string]string `json:"nodes"`
			PrivateChains map[string]struct {
				RpcURL    string `json:"rpc_url"`
				Address   string `json:"address"`
				BlsPubkey string `json:"bls_pubkey"`
			} `json:"private_chains"`
		}
		if err := json.Unmarshal(data, &topology); err == nil {
			if cfg.ConfigFile == "" {
				cfg.ConfigFile = rpcNodesPath
			}

			var discoveredURLs []string
			if len(topology.ParentNodes) > 0 {
				var keys []string
				for k := range topology.ParentNodes {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if u := topology.ParentNodes[k].RpcURL; u != "" {
						discoveredURLs = append(discoveredURLs, u)
					}
				}
			} else if len(topology.Nodes) > 0 {
				var keys []string
				for k := range topology.Nodes {
					if strings.HasPrefix(k, "parent_node_") {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				for _, k := range keys {
					discoveredURLs = append(discoveredURLs, topology.Nodes[k])
				}
			}

			if len(discoveredURLs) > 0 {
				cfg.ParentURLs = discoveredURLs
			}

			if topology.PrivateChains != nil {
				if chainA, ok := topology.PrivateChains["chain_a"]; ok && chainA.RpcURL != "" {
					if cfg.Exec1URL == "" || strings.Contains(cfg.Exec1URL, "127.0.0.1:8646") {
						cfg.Exec1URL = chainA.RpcURL
					}
					if chainA.Address != "" && cfg.Exec1Address == "" {
						cfg.Exec1Address = chainA.Address
					}
				}
				if chainB, ok := topology.PrivateChains["chain_b"]; ok && chainB.RpcURL != "" {
					if cfg.Exec2URL == "" || strings.Contains(cfg.Exec2URL, "127.0.0.1:8647") {
						cfg.Exec2URL = chainB.RpcURL
					}
					if chainB.Address != "" && cfg.Exec2Address == "" {
						cfg.Exec2Address = chainB.Address
					}
				}
			}
		}
	}

	// 3. Allow environment variable overrides
	if pURLs := os.Getenv("PARENT_CHAIN_URLS"); pURLs != "" {
		var list []string
		for _, u := range strings.Split(pURLs, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				list = append(list, u)
			}
		}
		if len(list) > 0 {
			cfg.ParentURLs = list
		}
	} else if pURL := os.Getenv("PARENT_CHAIN_URL"); pURL != "" {
		cfg.ParentURLs = []string{pURL}
	}

	if e1 := os.Getenv("EXEC1_URL"); e1 != "" {
		cfg.Exec1URL = e1
	}
	if e2 := os.Getenv("EXEC2_URL"); e2 != "" {
		cfg.Exec2URL = e2
	}

	return cfg
}

func (c *ClusterConfig) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Parent Chain Nodes (%d):\n", len(c.ParentURLs)))
	for i, u := range c.ParentURLs {
		sb.WriteString(fmt.Sprintf("  • Node %d: %s\n", i, u))
	}
	sb.WriteString(fmt.Sprintf("Exec Cluster 1: %s (Address: %s, Keys: %d)\n", c.Exec1URL, c.Exec1Address, len(c.Exec1PrivateKeys)))
	sb.WriteString(fmt.Sprintf("Exec Cluster 2: %s (Address: %s, Keys: %d)\n", c.Exec2URL, c.Exec2Address, len(c.Exec2PrivateKeys)))
	sb.WriteString(fmt.Sprintf("Top-level Private Keys: %d\n", len(c.PrivateKeys)))
	if c.ConfigFile != "" {
		sb.WriteString(fmt.Sprintf("Source: %s\n", c.ConfigFile))
	} else {
		sb.WriteString("Source: Defaults / Environment Variables\n")
	}
	return sb.String()
}
