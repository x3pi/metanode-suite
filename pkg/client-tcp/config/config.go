package config

import (
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"time"

	"tool-test/pkg/bls"
	p_common "tool-test/pkg/common"
	"tool-test/pkg/logger"
	"tool-test/pkg/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

type RemoteChain struct {
	Name                    string `json:"name"`
	NationId                uint64 `json:"nation_id"`
	LocalContract           string `json:"local_contract"`
	ParentAddress           string `json:"parent_address"`
	ParentConnectionAddress string `json:"parent_connection_address"`
	RemoteContract          string `json:"remote_contract"`
	Privatekey              string `json:"private_key"`
	EthPrivateKey           string `json:"eth_private_key"`
}

type ClientConfig struct {
	PrivateKey_ string `json:"private_key"`

	ConnectionAddress_       string `json:"connection_address"`
	PublicConnectionAddress_ string `json:"public_connection_address"`

	Version_          string       `json:"version"`
	TransactionFeeHex string       `json:"transaction_fee"`
	TransactionFee    *uint256.Int `json:"-"`

	ParentAddress           string      `json:"parent_address"`
	ParentConnectionAddress interface{} `json:"parent_connection_address"`
	ParentConnectionType    string      `json:"parent_connection_type"`
	ChainId                 uint64      `json:"chain_id"`
	NationId                uint64      `json:"nation_id"`
	HttpRpc                 string      `json:"http_rpc"`

	// Supervisor fields (merged from config-1.json)
	LogPath string `json:"log_path"`
	// Demo ABI config
	DemoAbiPath_        string  `json:"demo_abi_path"`
	DemoContractAddress string  `json:"demo_contract_address"`
	DemoAbi             abi.ABI `json:"-"`
	EthPrivateKey       string  `json:"eth_private_key"`
}

func (c *ClientConfig) ConnectionAddress() string {
	return c.ConnectionAddress_
}

func (c *ClientConfig) PublicConnectionAddress() string {
	return c.PublicConnectionAddress_
}

func (c *ClientConfig) GetParentConnectionAddress() string {
	if c.ParentConnectionAddress == nil {
		return ""
	}
	switch v := c.ParentConnectionAddress.(type) {
	case string:
		return v
	case []interface{}:
		if len(v) > 0 {
			if str, ok := v[0].(string); ok {
				return str
			}
		}
		return ""
	default:
		logger.Error("Invalid parent connection address, type is " + reflect.TypeOf(v).String())
		return ""
	}
}

func (c *ClientConfig) Version() string {
	return c.Version_
}

func (c *ClientConfig) PrivateKey() []byte {
	return common.FromHex(c.PrivateKey_)
}

func (c *ClientConfig) Address() common.Address {
	_, _, address := bls.GenerateKeyPairFromSecretKey(c.PrivateKey_)
	return address
}

func (c *ClientConfig) NodeType() string {
	return p_common.CLIENT_CONNECTION_TYPE
}

func LoadConfig(configPath string) (types.Config, error) {
	// general config
	config := &ClientConfig{}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, config)
	if err != nil {
		return nil, err
	}
	config.TransactionFee = uint256.NewInt(0).SetBytes(common.FromHex(config.TransactionFeeHex))

	// Load ABI file if path is specified

	// Load Demo ABI if path is specified
	if config.DemoAbiPath_ != "" {
		demoAbiData, err := os.ReadFile(config.DemoAbiPath_)
		if err != nil {
			logger.Warn("Failed to read demo ABI file: %v", err)
		} else {
			parsedDemoAbi, err := abi.JSON(strings.NewReader(string(demoAbiData)))
			if err != nil {
				logger.Warn("Failed to parse demo ABI JSON: %v", err)
			} else {
				config.DemoAbi = parsedDemoAbi
			}
		}
	}

	return config, nil
}
