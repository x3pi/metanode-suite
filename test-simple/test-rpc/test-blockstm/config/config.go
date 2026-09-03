package config

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
)

type ContractData struct {
	ABI      string `json:"abi"`
	Bytecode string `json:"bytecode"`
}

type PrivateChainConfig struct {
	ChainID     int64    `json:"chain_id"`
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
}

type Config struct {
	RPCUrl        string                        `json:"rpc_url"`
	RPCNodes      map[string]string             `json:"rpc_nodes"`
	SyncNodes     map[string]string             `json:"sync_nodes"`
	ChainID       int64                         `json:"chain_id"`
	PrivateKey    string                        `json:"private_key"`
	PrivateKeys   []string                      `json:"private_keys"`
	PrivateChains map[string]PrivateChainConfig `json:"private_chains"`
	TargetChain   string                        `json:"target_chain"`
	Contracts     map[string]ContractData       `json:"contracts"`
}

// LoadConfig reads configPath, unmarshals it into Config, and applies Private Chain resolution
// if TARGET_CHAIN env or cfg.TargetChain in config.json is specified.
func LoadConfig(configPath string) (*Config, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("không thể đọc file cấu hình %s: %w", configPath, err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("không thể parse file cấu hình %s: %w", configPath, err)
	}

	if cfg.PrivateKey == "" && len(cfg.PrivateKeys) > 0 {
		cfg.PrivateKey = cfg.PrivateKeys[0]
	}

	// 1. Kiểm tra target chain selector từ env TARGET_CHAIN hoặc field target_chain trong config.json
	target := strings.TrimSpace(os.Getenv("TARGET_CHAIN"))
	if target == "" {
		target = strings.TrimSpace(cfg.TargetChain)
	}

	// 2. Nếu target được chỉ định (khác rỗng, khác "public", khác "root")
	if target != "" && strings.ToLower(target) != "public" && strings.ToLower(target) != "root" && strings.ToLower(target) != "default" {
		found := false
		targetLower := strings.ToLower(target)

		// Thử tìm theo key trong PrivateChains (ví dụ "chain_a", "chain_b", "chain_101", ...)
		if pChain, ok := cfg.PrivateChains[targetLower]; ok {
			applyPrivateChain(&cfg, targetLower, pChain)
			found = true
		} else {
			// Thử tìm theo định dạng "chain_X" hoặc so khớp ChainID
			for key, pChain := range cfg.PrivateChains {
				keyLower := strings.ToLower(key)
				if keyLower == targetLower ||
					keyLower == "chain_"+targetLower ||
					fmt.Sprintf("%d", pChain.ChainID) == targetLower {
					applyPrivateChain(&cfg, key, pChain)
					found = true
					break
				}
			}
		}

		if !found {
			fmt.Printf("⚠️ [TESTCONFIG] Không tìm thấy cấu hình cho chain '%s' trong private_chains, sử dụng cấu hình mặc định (Public Chain)\n", target)
		}
	} else {
		fmt.Printf("🌐 [TESTCONFIG] Đang chạy trên: Public Chain (RPC: %s)\n", cfg.RPCUrl)
	}

	// 3. Tự động truy vấn ChainID từ RPC nếu ChainID trong config = 0
	if cfg.ChainID == 0 && cfg.RPCUrl != "" {
		if client, err := ethclient.Dial(cfg.RPCUrl); err == nil {
			if realChainID, err := client.ChainID(context.Background()); err == nil && realChainID != nil {
				cfg.ChainID = realChainID.Int64()
			}
			client.Close()
		}
	}

	return &cfg, nil
}

func applyPrivateChain(cfg *Config, name string, pChain PrivateChainConfig) {
	if pChain.RPCUrl != "" {
		cfg.RPCUrl = pChain.RPCUrl
	}
	if pChain.ChainID != 0 {
		cfg.ChainID = pChain.ChainID
	}
	// Giữ nguyên 10 private_keys của public chain (vì genesis balance được cấp giống nhau)
	if len(cfg.PrivateKeys) == 0 && len(pChain.PrivateKeys) > 0 {
		cfg.PrivateKeys = pChain.PrivateKeys
	}
	if cfg.PrivateKey == "" && len(cfg.PrivateKeys) > 0 {
		cfg.PrivateKey = cfg.PrivateKeys[0]
	}
	// Nếu chuyển sang Private Chain, cập nhật lại RPCNodes thành RPC của private chain đó
	cfg.RPCNodes = map[string]string{
		name: cfg.RPCUrl,
	}
	fmt.Printf("🔗 [TESTCONFIG] Đã chuyển sang Private Chain '%s' (ChainID: %d, RPC: %s, %d keys)\n",
		name, cfg.ChainID, cfg.RPCUrl, len(cfg.PrivateKeys))
}

// GetChainIDBig returns ChainID as *big.Int
func (c *Config) GetChainIDBig() *big.Int {
	return big.NewInt(c.ChainID)
}
