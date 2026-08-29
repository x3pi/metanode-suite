package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ─── ANSI COLORS ─────────────────────────────────────────────────────────────
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[91m"
	ColorGreen  = "\033[92m"
	ColorYellow = "\033[93m"
	ColorBlue   = "\033[94m"
	ColorPurple = "\033[95m"
	ColorCyan   = "\033[96m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
)

var GatewayAddress = common.HexToAddress("0x0000000000000000000000000000000000001002")

const GatewayABI = `[
	{
		"inputs": [
			{"internalType": "uint256", "name": "destChainId", "type": "uint256"},
			{"internalType": "address", "name": "target", "type": "address"},
			{"internalType": "bytes", "name": "payload", "type": "bytes"},
			{"internalType": "uint256", "name": "assetId", "type": "uint256"},
			{"internalType": "uint256", "name": "value", "type": "uint256"},
			{"internalType": "uint256", "name": "tip", "type": "uint256"},
			{"internalType": "uint256", "name": "gasFee", "type": "uint256"},
			{"internalType": "uint8", "name": "hopCount", "type": "uint8"},
			{"internalType": "bool", "name": "ordered", "type": "bool"}
		],
		"name": "outbound",
		"outputs": [{"internalType": "bytes32", "name": "messageId", "type": "bytes32"}],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

const CaroContractABI = `[
	{"inputs":[{"internalType":"address","name":"playerX","type":"address"},{"internalType":"address","name":"playerO","type":"address"}],"name":"createGame","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"payable","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"gameId","type":"uint256"},{"internalType":"uint8","name":"row","type":"uint8"},{"internalType":"uint8","name":"col","type":"uint8"},{"internalType":"uint8","name":"playerCell","type":"uint8"}],"name":"playMove","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"gameId","type":"uint256"}],"name":"getBoard","outputs":[{"internalType":"uint8[3][3]","name":"boardResult","type":"uint8[3][3]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"games","outputs":[{"internalType":"address","name":"playerX","type":"address"},{"internalType":"address","name":"playerO","type":"address"},{"internalType":"uint256","name":"wagerAmount","type":"uint256"},{"internalType":"uint8","name":"currentTurn","type":"uint8"},{"internalType":"uint8","name":"status","type":"uint8"},{"internalType":"uint8","name":"moveCount","type":"uint8"}],"stateMutability":"view","type":"function"}
]`

const CaroBytecodeHex = "6080604052600180553480156012575f5ffd5b5061171d806100205f395ff3fe608060405260043610610049575f3560e01c8063117a5b901461004d5780632dc77c701461008e57806345e09e54146100ca578063a6f979ff14610106578063b135bbb014610136575b5f5ffd5b348015610058575f5ffd5b50610073600480360381019061006e9190610f9a565b610160565b604051610085969594939291906110e7565b60405180910390f35b348015610099575f5ffd5b506100b460048036038101906100af9190611170565b6101fb565b6040516100c191906111ee565b60405180910390f35b3480156100d5575f5ffd5b506100f060048036038101906100eb9190610f9a565b610729565b6040516100fd9190611342565b60405180910390f35b610120600480360381019061011b9190611386565b610820565b60405161012d91906113c4565b60405180910390f35b348015610141575f5ffd5b5061014a610978565b60405161015791906113c4565b60405180910390f35b5f602052805f5260405f205f91509050805f015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690806001015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690806002015490806006015f9054906101000a900460ff16908060060160019054906101000a900460ff16908060060160029054906101000a900460ff16905086565b5f5f5f5f8781526020019081526020015f2090505f600381111561022257610221611013565b5b8160060160019054906101000a900460ff16600381111561024657610245611013565b5b14610286576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161027d90611437565b60405180910390fd5b60038560ff1610801561029c575060038460ff16105b6102db576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016102d29061149f565b60405180910390fd5b5f60028111156102ee576102ed611013565b5b816003018660ff1660038110610307576103066114bd565b5b018560ff166003811061031d5761031c6114bd565b5b602091828204019190069054906101000a900460ff16600281111561034557610344611013565b5b14610385576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161037c90611534565b60405180910390fd5b5f8360ff16600281111561039c5761039b611013565b5b9050816006015f9054906101000a900460ff1660028111156103c1576103c0611013565b5b8160028111156103d4576103d3611013565b5b14610414576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161040b9061159c565b60405180910390fd5b80826003018760ff166003811061042e5761042d6114bd565b5b018660ff1660038110610444576104436114bd565b5b602091828204019190066101000a81548160ff0219169083600281111561046e5761046d611013565b5b021790555081600601600281819054906101000a900460ff1680929190610494906115e7565b91906101000a81548160ff021916908360ff16021790555050867f186abf997d1d690189496edbd4fb7615ab41008858e22b21c45e4175e8fa8ca28787846040516104e19392919061160f565b60405180910390a26104f3878261097e565b1561062c576001600281111561050c5761050b611013565b5b81600281111561051f5761051e611013565b5b1461052b57600261052e565b60015b8260060160016101000a81548160ff0219169083600381111561055457610553611013565b5b02179055505f6001600281111561056e5761056d611013565b5b82600281111561058157610580611013565b5b146105af57826001015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff166105d3565b825f015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff165b9050877f69137706d65cad5bb2a9a944e02d6c3474190ffcaf7c1eb0e70474b5a52ecc628460060160019054906101000a900460ff1683604051610618929190611644565b60405180910390a260019350505050610721565b60098260060160029054906101000a900460ff1660ff16036106bb5760038260060160016101000a81548160ff021916908360038111156106705761066f611013565b5b0217905550867f69137706d65cad5bb2a9a944e02d6c3474190ffcaf7c1eb0e70474b5a52ecc6260035f6040516106a8929190611644565b60405180910390a2600192505050610721565b600160028111156106cf576106ce611013565b5b8160028111156106e2576106e1611013565b5b146106ee5760016106f1565b60025b826006015f6101000a81548160ff0219169083600281111561071657610715611013565b5b02179055505f925050505b949350505050565b610731610f14565b5f5f5f8481526020019081526020015f2090505f5f90505b60038160ff161015610819575f5f90505b60038160ff16101561080b57826003018260ff166003811061077f5761077e6114bd565b5b018160ff1660038110610795576107946114bd565b5b602091828204019190069054906101000a900460ff1660028111156107bd576107bc611013565b5b848360ff16600381106107d3576107d26114bd565b5b60200201518260ff16600381106107ed576107ec6114bd565b5b602002019060ff16908160ff1681525050808060010191505061075a565b508080600101915050610749565b5050919050565b5f5f60015f8154809291906108349061166b565b9190505590505f5f5f8381526020019081526020015f20905084815f015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555083816001015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055503481600201819055506001816006015f6101000a81548160ff02191690836002811115610900576108ff611013565b5b02179055505f8160060160016101000a81548160ff0219169083600381111561092c5761092b611013565b5b0217905550817f6200407c0ea392b8107b21a9be480acd41fda186d04bed28cc7da2d4b53d56e2868634604051610965939291906116b2565b60405180910390a2819250505092915050565b60015481565b5f5f5f5f8581526020019081526020015f2090505f5f90505b60038160ff161015610c64578360028111156109b6576109b5611013565b5b826003018260ff16600381106109cf576109ce6114bd565b5b015f600381106109e2576109e16114bd565b5b602091828204019190069054906101000a900460ff166002811115610a0a57610a09611013565b5b148015610a7c5750836002811115610a2557610a24611013565b5b826003018260ff1660038110610a3e57610a3d6114bd565b5b01600160038110610a5257610a516114bd565b5b602091828204019190069054906101000a900460ff166002811115610a7a57610a79611013565b5b145b8015610aed5750836002811115610a9657610a95611013565b5b826003018260ff1660038110610aaf57610aae6114bd565b5b01600260038110610ac357610ac26114bd565b5b602091828204019190069054906101000a900460ff166002811115610aeb57610aea611013565b5b145b15610afd57600192505050610f0e565b836002811115610b1057610b0f611013565b5b826003015f60038110610b2657610b256114bd565b5b018260ff1660038110610b3c57610b3b6114bd565b5b602091828204019190069054906101000a900460ff166002811115610b6457610b63611013565b5b148015610bd65750836002811115610b7f57610b7e611013565b5b82600301600160038110610b9657610b956114bd565b5b018260ff1660038110610bac57610bab6114bd565b5b602091828204019190069054906101000a900460ff166002811115610bd457610bd3611013565b5b145b8015610c475750836002811115610bf057610bef611013565b5b82600301600260038110610c0757610c066114bd565b5b018260ff1660038110610c1d57610c1c6114bd565b5b602091828204019190069054906101000a900460ff166002811115610c4557610c44611013565b5b145b15610c5757600192505050610f0e565b8080600101915050610997565b50826002811115610c7857610c77611013565b5b816003015f60038110610c8e57610c8d6114bd565b5b015f60038110610ca157610ca06114bd565b5b602091828204019190069054906101000a900460ff166002811115610cc957610cc8611013565b5b148015610d395750826002811115610ce457610ce3611013565b5b81600301600160038110610cfb57610cfa6114bd565b5b01600160038110610d0f57610d0e6114bd565b5b602091828204019190069054906101000a900460ff166002811115610d3757610d36611013565b5b145b8015610da85750826002811115610d5357610d52611013565b5b81600301600260038110610d6a57610d696114bd565b5b01600260038110610d7e57610d7d6114bd565b5b602091828204019190069054906101000a900460ff166002811115610da657610da5611013565b5b145b15610db7576001915050610f0e565b826002811115610dca57610dc9611013565b5b816003015f60038110610de057610ddf6114bd565b5b01600260038110610df457610df36114bd565b5b602091828204019190069054906101000a900460ff166002811115610e1c57610e1b611013565b5b148015610e8c5750826002811115610e3757610e36611013565b5b81600301600160038110610e4e57610e4d6114bd565b5b01600160038110610e6257610e616114bd565b5b602091828204019190069054906101000a900460ff166002811115610e8a57610e89611013565b5b145b8015610efa5750826002811115610ea657610ea5611013565b5b81600301600260038110610ebd57610ebc6114bd565b5b015f60038110610ed057610ecf6114bd565b5b602091828204019190069054906101000a900460ff166002811115610ef857610ef7611013565b5b145b15610f09576001915050610f0e565b5f9150505b92915050565b60405180606001604052806003905b610f2b610f41565b815260200190600190039081610f235790505090565b6040518060600160405280600390602082028036833780820191505090505090565b5f5ffd5b5f819050919050565b610f7981610f67565b8114610f83575f5ffd5b50565b5f81359050610f9481610f70565b92915050565b5f60208284031215610faf57610fae610f63565b5b5f610fbc84828501610f86565b91505092915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610fee82610fc5565b9050919050565b610ffe81610fe4565b82525050565b61100d81610f67565b82525050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52602160045260245ffd5b6003811061105157611050611013565b5b50565b5f81905061106182611040565b919050565b5f61107082611054565b9050919050565b61108081611066565b82525050565b6004811061109757611096611013565b5b50565b5f8190506110a782611086565b919050565b5f6110b68261109a565b9050919050565b6110c6816110ac565b82525050565b5f60ff82169050919050565b6110e1816110cc565b82525050565b5f60c0820190506110fa5f830189610ff5565b6111076020830188610ff5565b6111146040830187611004565b6111216060830186611077565b61112e60808301856110bd565b61113b60a08301846110d8565b979650505050505050565b61114f816110cc565b8114611159575f5ffd5b50565b5f8135905061116a81611146565b92915050565b5f5f5f5f6080858703121561118857611187610f63565b5b5f61119587828801610f86565b94505060206111a68782880161115c565b93505060406111b78782880161115c565b92505060606111c88782880161115c565b91505092959194509250565b5f8115159050919050565b6111e8816111d4565b82525050565b5f6020820190506112015f8301846111df565b92915050565b5f60039050919050565b5f81905092915050565b5f819050919050565b5f60039050919050565b5f81905092915050565b5f819050919050565b61124a816110cc565b82525050565b5f61125b8383611241565b60208301905092915050565b5f602082019050919050565b61127c81611224565b611286818461122e565b925061129182611238565b805f5b838110156112c15781516112a88782611250565b96506112b383611267565b925050600181019050611294565b505050505050565b5f6112d48383611273565b60608301905092915050565b5f602082019050919050565b6112f581611207565b6112ff8184611211565b925061130a8261121b565b805f5b8381101561133a57815161132187826112c9565b965061132c836112e0565b92505060018101905061130d565b505050505050565b5f610120820190506113565f8301846112ec565b92915050565b61136581610fe4565b811461136f575f5ffd5b50565b5f813590506113808161135c565b92915050565b5f5f6040838503121561139c5761139b610f63565b5b5f6113a985828601611372565b92505060206113ba85828601611372565b9150509250929050565b5f6020820190506113d75f830184611004565b92915050565b5f82825260208201905092915050565b7f47616d65206e6f742061637469766500000000000000000000000000000000005f82015250565b5f611421600f836113dd565b915061142c826113ed565b602082019050919050565b5f6020820190508181035f83015261144e81611415565b9050919050565b7f496e76616c696420636f6f7264696e61746573000000000000000000000000005f82015250565b5f6114896013836113dd565b915061149482611455565b602082019050919050565b5f6020820190508181035f8301526114b68161147d565b9050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52603260045260245ffd5b7f43656c6c20616c7265616479206f6363757069656400000000000000000000005f82015250565b5f61151e6015836113dd565b9150611529826114ea565b602082019050919050565b5f6020820190508181035f83015261154b81611512565b9050919050565b7f4e6f7420796f7572207475726e000000000000000000000000000000000000005f82015250565b5f611586600d836113dd565b915061159182611552565b602082019050919050565b5f6020820190508181035f8301526115b38161157a565b9050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6115f1826110cc565b915060ff8203611604576116036115ba565b5b600182019050919050565b5f6060820190506116225f8301866110d8565b61162f60208301856110d8565b61163c6040830184611077565b949350505050565b5f6040820190506116575f8301856110bd565b6116646020830184610ff5565b9392505050565b5f61167582610f67565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff82036116a7576116a66115ba565b5b600182019050919050565b5f6060820190506116c55f830186610ff5565b6116d26020830185610ff5565b6116df6040830184611004565b94935050505056fea264697066735822122001daabfd2cc81a490642250d6cb89763585b96f795b78de042553da28f9e761b64736f6c63430008230033"

// ─── RPC Helpers ─────────────────────────────────────────────────────────────
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID int `json:"id"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func callRPC(url, method string, params []interface{}) (json.RawMessage, error) {
	reqBody, _ := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var jsonResp JSONRPCResponse
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, err
	}
	if jsonResp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", jsonResp.Error.Message)
	}
	return jsonResp.Result, nil
}

func getNonce(url, address string) (uint64, error) {
	res, err := callRPC(url, "eth_getTransactionCount", []interface{}{address, "pending"})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func sendRawTransaction(url string, rawTx []byte) (common.Hash, error) {
	hexTx := hexutil.Encode(rawTx)
	res, err := callRPC(url, "eth_sendRawTransaction", []interface{}{hexTx})
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(res, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

func ethCall(url string, to common.Address, data []byte) (string, error) {
	callObj := map[string]string{
		"to":   to.Hex(),
		"data": hexutil.Encode(data),
	}
	res, err := callRPC(url, "eth_call", []interface{}{callObj, "latest"})
	if err != nil {
		return "", err
	}
	var out string
	json.Unmarshal(res, &out)
	return out, nil
}

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) {
	start := time.Now()
	for time.Since(start) < timeout {
		res, err := callRPC(url, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ─── CARO BOARD STATE ────────────────────────────────────────────────────────
type CaroBoard struct {
	Grid   [3][3]uint8
	Status int
}

func (b *CaroBoard) PrintBoard(title string) {
	fmt.Printf("\n  ┌─────────────────────────────────────────────────────────┐\n")
	displayTitle := title
	if len(displayTitle) > 40 {
		displayTitle = displayTitle[:40]
	}
	fmt.Printf("  │  %-53s  │\n", ColorBold+displayTitle+ColorReset)
	fmt.Printf("  ├─────────────────────────────────────────────────────────┤\n")
	fmt.Printf("  │                 Cột 0         Cột 1         Cột 2       │\n")
	fmt.Printf("  │             ┌───────────┬───────────┬───────────┐       │\n")

	for r := 0; r < 3; r++ {
		fmt.Printf("  │    Hàng %d   │", r)
		for c := 0; c < 3; c++ {
			switch b.Grid[r][c] {
			case 1:
				fmt.Printf("   %s[ X ]%s   │", ColorRed+ColorBold, ColorReset)
			case 2:
				fmt.Printf("   %s[ O ]%s   │", ColorCyan+ColorBold, ColorReset)
			default:
				fmt.Printf("   %s[ · ]%s   │", ColorDim, ColorReset)
			}
		}
		fmt.Printf("       │\n")
		if r < 2 {
			fmt.Printf("  │             ├───────────┼───────────┼───────────┤       │\n")
		}
	}
	fmt.Printf("  │             └───────────┴───────────┴───────────┘       │\n")
	fmt.Printf("  └─────────────────────────────────────────────────────────┘\n")
}

func (b *CaroBoard) FindBestAIMove() (int, int) {
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if b.Grid[r][c] == 0 {
				return r, c
			}
		}
	}
	return 0, 0
}

// ─── Config Types ────────────────────────────────────────────────────────────
type ChainEntry struct {
	ChainID     uint64
	RpcUrl      string
	PrivateKeys []string
}

type PrivateChainJson struct {
	ChainID     uint64   `json:"chain_id"`
	RpcUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
}

type ConfigStructure struct {
	PrivateChains map[string]PrivateChainJson `json:"private_chains"`
	Nodes         map[string]string           `json:"nodes"`
	PrivateKeys   []string                    `json:"private_keys"`
}

func sanitizeKey(k string) string {
	k = strings.TrimSpace(k)
	return strings.TrimPrefix(k, "0x")
}

func loadAvailableChains(configFilePath string) (map[string]ChainEntry, error) {
	paths := []string{
		configFilePath,
		"../config.json",
		"../../config.json",
		"/tmp/private_chains.json",
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}

		var cfg ConfigStructure
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		chains := make(map[string]ChainEntry)

		for name, c := range cfg.PrivateChains {
			var keys []string
			for _, k := range c.PrivateKeys {
				if sanitized := sanitizeKey(k); sanitized != "" {
					keys = append(keys, sanitized)
				}
			}
			entry := ChainEntry{
				ChainID:     c.ChainID,
				RpcUrl:      c.RpcUrl,
				PrivateKeys: keys,
			}
			chains[fmt.Sprintf("%d", c.ChainID)] = entry
			chains[strings.ToLower(name)] = entry
		}

		for cidStr, rpc := range cfg.Nodes {
			if _, exists := chains[cidStr]; !exists {
				var cid uint64
				fmt.Sscanf(cidStr, "%d", &cid)
				entry := ChainEntry{
					ChainID:     cid,
					RpcUrl:      rpc,
					PrivateKeys: []string{},
				}
				chains[cidStr] = entry
				chains[fmt.Sprintf("chain_%s", cidStr)] = entry
			}
		}

		if len(chains) > 0 {
			return chains, nil
		}
	}
	return nil, fmt.Errorf("không tìm thấy file cấu hình hợp lệ (đã thử: %v)", paths)
}

func main() {
	var targetFrom, targetTo, configPath string
	interactiveFlag := flag.Bool("interactive", false, "Chế độ người chơi tự tay nhập nước đi từ bàn phím")

	flag.StringVar(&targetFrom, "from", "101", "ID Chain Player X (ví dụ: -from 101)")
	flag.StringVar(&targetFrom, "src", "101", "Alias của -from")
	flag.StringVar(&targetFrom, "source", "101", "Alias của -from")

	flag.StringVar(&targetTo, "to", "102", "ID Chain Player O & Game Contract (ví dụ: -to 102)")
	flag.StringVar(&targetTo, "dst", "102", "Alias của -to")
	flag.StringVar(&targetTo, "dest", "102", "Alias của -to")

	flag.StringVar(&configPath, "config", "../config.json", "Đường dẫn file config.json")

	flag.Usage = func() {
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
		fmt.Println(ColorBold + "🎮 HƯỚNG DẪN CHƠI GAME CARO XUYÊN CHUỖI" + ColorReset)
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
		fmt.Println("Cú pháp dùng Flag:")
		fmt.Println("  go run . -from 101 -to 102")
		fmt.Println("  go run . -from 101 -to 103 -interactive")
		fmt.Println("\nCú pháp truyền nhanh (Positional Args):")
		fmt.Println("  go run . 101 102")
		fmt.Println("  go run . 103 101")
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	}

	flag.Parse()

	posArgs := flag.Args()
	if len(posArgs) >= 1 {
		targetFrom = posArgs[0]
	}
	if len(posArgs) >= 2 {
		targetTo = posArgs[1]
	}

	defaultKeyA := "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
	defaultKeyB := "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a"

	availableChains, _ := loadAvailableChains(configPath)

	fromEntry, okFrom := availableChains[strings.ToLower(targetFrom)]
	if !okFrom {
		var cid uint64 = 101
		fmt.Sscanf(targetFrom, "%d", &cid)
		fromEntry = ChainEntry{ChainID: cid, RpcUrl: "http://127.0.0.1:8546"}
	}

	toEntry, okTo := availableChains[strings.ToLower(targetTo)]
	if !okTo {
		var cid uint64 = 102
		fmt.Sscanf(targetTo, "%d", &cid)
		toEntry = ChainEntry{ChainID: cid, RpcUrl: "http://127.0.0.1:8547"}
	}

	keyA := defaultKeyA
	if len(fromEntry.PrivateKeys) > 0 {
		keyA = fromEntry.PrivateKeys[0]
	}
	keyB := defaultKeyB
	if len(toEntry.PrivateKeys) > 0 {
		keyB = toEntry.PrivateKeys[0]
	}

	privKey1, _ := crypto.HexToECDSA(keyA)
	player1Addr := crypto.PubkeyToAddress(privKey1.PublicKey)

	privKey2, _ := crypto.HexToECDSA(keyB)
	player2Addr := crypto.PubkeyToAddress(privKey2.PublicKey)

	parsedGatewayABI, _ := abi.JSON(strings.NewReader(GatewayABI))
	parsedCaroABI, _ := abi.JSON(strings.NewReader(CaroContractABI))

	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf(ColorCyan+ColorBold+"🎮 GAME CARO XUYÊN CHUỖI PURE CLIENT (CHAIN %d ➔ CHAIN %d)\n"+ColorReset, fromEntry.ChainID, toEntry.ChainID)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf("👥 NGƯỜI CHƠI (FLAGS: -from %d -to %d):\n", fromEntry.ChainID, toEntry.ChainID)
	fmt.Printf("   ├─ %sNgười chơi X%s (--from %d): %s (@ %s)\n", ColorRed+ColorBold, ColorReset, fromEntry.ChainID, player1Addr.Hex(), fromEntry.RpcUrl)
	fmt.Printf("   └─ %sNgười chơi O%s (--to %d):   %s (@ %s)\n", ColorCyan+ColorBold, ColorReset, toEntry.ChainID, player2Addr.Hex(), toEntry.RpcUrl)

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 1: DEPLOY SMART CONTRACT CARO TRÊN CHAIN ĐÍCH (DESTINATION CHAIN)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Printf("\n"+ColorBold+"📜 BƯỚC 1: KHỞI TẠO BÀN CỜ CARO TRÊN CHAIN %d"+ColorReset+"\n", toEntry.ChainID)
	caroBytecode, _ := hexutil.Decode("0x" + CaroBytecodeHex)
	nonceDeploy, _ := getNonce(toEntry.RpcUrl, player2Addr.Hex())

	deployTx := types.NewContractCreation(nonceDeploy, big.NewInt(0), 4_000_000, big.NewInt(1e9), caroBytecode)
	signedDeployTx, _ := types.SignTx(deployTx, types.NewEIP155Signer(new(big.Int).SetUint64(toEntry.ChainID)), privKey2)
	rawDeployBytes, _ := signedDeployTx.MarshalBinary()

	deployHash, errDeploy := sendRawTransaction(toEntry.RpcUrl, rawDeployBytes)
	if errDeploy != nil {
		fmt.Printf("❌ Lỗi deploy Caro contract: %v\n", errDeploy)
		return
	}
	waitForReceipt(toEntry.RpcUrl, deployHash, 10*time.Second)
	caroContractAddr := crypto.CreateAddress(player2Addr, nonceDeploy)
	fmt.Printf("   ✅ Smart Contract Caro Address trên Chain %d: %s%s%s\n", toEntry.ChainID, ColorPurple, caroContractAddr.Hex(), ColorReset)

	// Khởi tạo Game #1 trên Smart Contract
	createGameData, _ := parsedCaroABI.Pack("createGame", player1Addr, player2Addr)
	time.Sleep(1 * time.Second)
	nonceCreate, _ := getNonce(toEntry.RpcUrl, player2Addr.Hex())
	txCreate := types.NewTransaction(nonceCreate, caroContractAddr, big.NewInt(0), 500000, big.NewInt(1e9), createGameData)
	signedCreate, _ := types.SignTx(txCreate, types.NewEIP155Signer(new(big.Int).SetUint64(toEntry.ChainID)), privKey2)
	rawCreate, _ := signedCreate.MarshalBinary()
	hashCreate, _ := sendRawTransaction(toEntry.RpcUrl, rawCreate)
	waitForReceipt(toEntry.RpcUrl, hashCreate, 10*time.Second)
	fmt.Printf("   ✅ Game ID #1 đã được tạo trên Smart Contract!\n")

	board := &CaroBoard{}
	board.PrintBoard("BÀN CỜ BAN ĐẦU (0/9 NƯỚC ĐI)")

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 2: TIẾN TRÌNH THI ĐẤU XUYÊN CHUỖI (PURE CLIENT)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "⚔️ BƯỚC 2: TIẾN TRÌNH THI ĐẤU (CLIENT CHỈ GỬI - RELAYER TỰ ĐỘNG CHUYỂN TIẾP)" + ColorReset)

	scriptedMoves := [][2]int{
		{1, 1}, // X (Chain Nguồn): Tâm
		{0, 1}, // O (Chain Đích): Trên
		{0, 0}, // X (Chain Nguồn): Góc trái trên
		{2, 1}, // O (Chain Đích): Dưới
		{2, 2}, // X (Chain Nguồn): Góc phải dưới ➔ X Thắng chéo (0,0)-(1,1)-(2,2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	turnCount := 1
	currentTurnCell := 1 // 1: X (Chain Nguồn), 2: O (Chain Đích)

	for board.Status == 0 {
		var row, col int

		if currentTurnCell == 1 {
			// LƯỢT CỦA PLAYER X (CHAIN NGUỒN ➔ GỬI OUTBOUND SANG CHAIN ĐÍCH)
			if *interactiveFlag {
				for {
					fmt.Printf("\n👉 %sLƯỢT CỦA BẠN (QUÂN [ X ] TRÊN CHAIN %d):%s Nhập 'hàng cột' (0-2), ví dụ '1 1': ", ColorRed+ColorBold, fromEntry.ChainID, ColorReset)
					if !scanner.Scan() {
						break
					}
					line := strings.TrimSpace(scanner.Text())
					parts := strings.Fields(line)
					if len(parts) != 2 {
						fmt.Printf("   %s⚠️ Định dạng sai! Vui lòng nhập 2 số cách nhau bởi khoảng trắng.%s\n", ColorYellow, ColorReset)
						continue
					}
					r, errR := strconv.Atoi(parts[0])
					c, errC := strconv.Atoi(parts[1])
					if errR != nil || errC != nil || r < 0 || r > 2 || c < 0 || c > 2 {
						fmt.Printf("   %s⚠️ Tọa độ không hợp lệ! Hàng và Cột phải từ 0 đến 2.%s\n", ColorYellow, ColorReset)
						continue
					}
					if board.Grid[r][c] != 0 {
						fmt.Printf("   %s⚠️ Ô (%d, %d) đã có quân chiếm! Hãy chọn ô khác.%s\n", ColorYellow, r, c, ColorReset)
						continue
					}
					row, col = r, c
					break
				}
			} else {
				scriptIdx := turnCount - 1
				if scriptIdx < len(scriptedMoves) {
					row = scriptedMoves[scriptIdx][0]
					col = scriptedMoves[scriptIdx][1]
				} else {
					row, col = board.FindBestAIMove()
				}
			}

			fmt.Printf("\n▶ %sNƯỚC %d: Người chơi X (Chain %d) ĐÁNH Ô (%d, %d)%s\n", ColorBold, turnCount, fromEntry.ChainID, row, col, ColorReset)

			// 1. Pack playMove payload
			playMovePayload, _ := parsedCaroABI.Pack("playMove", big.NewInt(1), uint8(row), uint8(col), uint8(1))

			// 2. Client gọi outbound trên Gateway Chain Nguồn
			tipAmount := big.NewInt(1e16)                   // 0.01 MTN Tip
			gasFeeAmount := big.NewInt(100_000_000_000_000) // 0.0001 MTN Gas Fee
			totalBurn := new(big.Int).Add(tipAmount, gasFeeAmount)

			outboundData, _ := parsedGatewayABI.Pack("outbound",
				new(big.Int).SetUint64(toEntry.ChainID),
				caroContractAddr,
				playMovePayload,
				big.NewInt(0),
				big.NewInt(0),
				tipAmount,
				gasFeeAmount,
				uint8(1),
				false,
			)
			time.Sleep(500 * time.Millisecond)
			nonceA, _ := getNonce(fromEntry.RpcUrl, player1Addr.Hex())

			txOutbound := types.NewTransaction(nonceA, GatewayAddress, totalBurn, 500000, big.NewInt(1e9), outboundData)
			signedTxOutbound, _ := types.SignTx(txOutbound, types.NewEIP155Signer(new(big.Int).SetUint64(fromEntry.ChainID)), privKey1)
			rawTxBytes, _ := signedTxOutbound.MarshalBinary()

			txHashOutbound, errSend := sendRawTransaction(fromEntry.RpcUrl, rawTxBytes)
			if errSend != nil {
				fmt.Printf("   ❌ Lỗi gửi outbound trên Chain %d: %v\n", fromEntry.ChainID, errSend)
				return
			}
			fmt.Printf("   🚀 Lệnh nộp lên Gateway Chain %d (Tx: %s) ✅\n", fromEntry.ChainID, txHashOutbound.Hex())
			waitForReceipt(fromEntry.RpcUrl, txHashOutbound, 10*time.Second)
			fmt.Printf("   ⏳ Client đứng đợi Relayer Daemon ngầm chuyển tiếp sang Chain %d...\n", toEntry.ChainID)

			// 3. Client polling Smart Contract trên Chain Đích để xem nước đi đã được thực thi chưa
			synced := false
			for attempt := 0; attempt < 60; attempt++ {
				time.Sleep(1 * time.Second)
				getBoardData, _ := parsedCaroABI.Pack("getBoard", big.NewInt(1))
				resHex, errCall := ethCall(toEntry.RpcUrl, caroContractAddr, getBoardData)
				if errCall == nil && resHex != "" && resHex != "0x" {
					resBytes := common.FromHex(resHex)
					out, unpackErr := parsedCaroABI.Unpack("getBoard", resBytes)
					if unpackErr == nil && len(out) > 0 {
						gridArr := out[0].([3][3]uint8)
						if gridArr[row][col] == 1 {
							synced = true
							board.Grid[row][col] = 1
							fmt.Printf("   %s🎉 RELAYER ĐÃ CHUYỂN THÀNH CÔNG! Smart Contract Chain %d đã nhận nước đi (%d, %d)!%s\n", ColorGreen, toEntry.ChainID, row, col, ColorReset)
							break
						}
					}
				}
				fmt.Printf(".")
			}

			if !synced {
				fmt.Printf("\n   ❌ Timeout chờ Relayer chuyển nước đi.\n")
				return
			}

		} else {
			// LƯỢT CỦA PLAYER O (CHAIN ĐÍCH)
			scriptIdx := turnCount - 1
			if scriptIdx < len(scriptedMoves) {
				row = scriptedMoves[scriptIdx][0]
				col = scriptedMoves[scriptIdx][1]
			} else {
				row, col = board.FindBestAIMove()
			}

			fmt.Printf("\n▶ %sNƯỚC %d: Người chơi O (Chain %d) ĐÁNH Ô (%d, %d)%s\n", ColorBold, turnCount, toEntry.ChainID, row, col, ColorReset)

			// Player O gọi trực tiếp Smart Contract trên Chain Đích
			playMoveData, _ := parsedCaroABI.Pack("playMove", big.NewInt(1), uint8(row), uint8(col), uint8(2))
			time.Sleep(500 * time.Millisecond)
			nonceB, _ := getNonce(toEntry.RpcUrl, player2Addr.Hex())

			txMoveB := types.NewTransaction(nonceB, caroContractAddr, big.NewInt(0), 500000, big.NewInt(1e9), playMoveData)
			signedTxB, _ := types.SignTx(txMoveB, types.NewEIP155Signer(new(big.Int).SetUint64(toEntry.ChainID)), privKey2)
			rawTxB, _ := signedTxB.MarshalBinary()

			txHashB, _ := sendRawTransaction(toEntry.RpcUrl, rawTxB)
			waitForReceipt(toEntry.RpcUrl, txHashB, 10*time.Second)
			board.Grid[row][col] = 2
			fmt.Printf("   ✅ Đã đánh trực tiếp trên Chain %d (Tx: %s)!\n", toEntry.ChainID, txHashB.Hex())
		}

		// Kiểm tra trạng thái Game trên Smart Contract
		gamesData, _ := parsedCaroABI.Pack("games", big.NewInt(1))
		gamesResHex, _ := ethCall(toEntry.RpcUrl, caroContractAddr, gamesData)
		if gamesResHex != "" && gamesResHex != "0x" {
			gamesResBytes := common.FromHex(gamesResHex)
			gamesOut, errUnpackGames := parsedCaroABI.Unpack("games", gamesResBytes)
			if errUnpackGames == nil && len(gamesOut) >= 5 {
				gameStatus := int(gamesOut[4].(uint8))
				board.Status = gameStatus
			}
		}

		board.PrintBoard(fmt.Sprintf("BÀN CỜ SAU NƯỚC %d (%s)", turnCount, map[int]string{1: "Quân [ X ]", 2: "Quân [ O ]"}[currentTurnCell]))

		if board.Status == 1 {
			fmt.Printf("\n🎉 %sCHIẾN THẮNG TUYỆT ĐỐI! Người chơi X (Chain %d) ĐÃ THẮNG TRÊN SMART CONTRACT CHAIN %d!%s\n", ColorGreen+ColorBold, fromEntry.ChainID, toEntry.ChainID, ColorReset)
			break
		} else if board.Status == 2 {
			fmt.Printf("\n🎉 %sCHIẾN THẮNG! Người chơi O (Chain %d) ĐÃ THẮNG TRÊN SMART CONTRACT!%s\n", ColorGreen+ColorBold, toEntry.ChainID, ColorReset)
			break
		} else if board.Status == 3 {
			fmt.Printf("\n🤝 %sKẾT QUẢ HÒA! BÀN CỜ ĐÃ ĐIỀN ĐỦ 9 Ô!%s\n", ColorYellow+ColorBold, ColorReset)
			break
		}

		// Đổi lượt
		if currentTurnCell == 1 {
			currentTurnCell = 2
		} else {
			currentTurnCell = 1
		}
		turnCount++
	}

	fmt.Println("\n" + ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf(ColorGreen+ColorBold+"🏆 TRẬN ĐẤU CARO XUYÊN CHUỖI (CHAIN %d ➔ CHAIN %d) THÀNH CÔNG 100%%!\n"+ColorReset, fromEntry.ChainID, toEntry.ChainID)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
}
