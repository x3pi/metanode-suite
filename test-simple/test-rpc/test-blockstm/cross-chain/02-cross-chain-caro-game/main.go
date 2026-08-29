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
	{"inputs":[{"internalType":"uint256","name":"gameId","type":"uint256"},{"internalType":"uint8","name":"row","type":"uint8"},{"internalType":"uint8","name":"col","type":"uint8"},{"internalType":"uint8","name":"playerRole","type":"uint8"}],"name":"playMove","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"gameId","type":"uint256"}],"name":"getBoard","outputs":[{"internalType":"uint8[3][3]","name":"","type":"uint8[3][3]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"games","outputs":[{"internalType":"address","name":"playerX","type":"address"},{"internalType":"address","name":"playerO","type":"address"},{"internalType":"uint8","name":"currentTurn","type":"uint8"},{"internalType":"uint8","name":"moveCount","type":"uint8"},{"internalType":"uint8","name":"status","type":"uint8"}],"stateMutability":"view","type":"function"}
]`

const CaroBytecodeHex = "608060405234801561000f575f80fd5b506109968061001d5f395ff3fe608060405234801561000f575f80fd5b506004361061004a575f3560e01c806307611ab41461004e5780631db9c17e1461007957806371cb7c5414610099578063b40097f4146100c4575b5f80fd5b610064600480360381019061005f9190610738565b6100e4565b604051610070919061079f565b60405180910390f35b610087600480360381019061008291906107b7565b610118565b6040516100909190610842565b60405180910390f35b6100af60048036038101906100aa9190610886565b61017d565b6040516100bb919061079f565b60405180910390f35b6100e260048036038101906100dd91906108b3565b610260565b005b5f8060015f8481526020019081526020015f2060020154915050919050565b60015f8281526020019081526020015f20805460010190555f805f80808060015f8881526020019081526020015f2080546001600160a01b031916968816969096179095556001860180546001600160a01b03191694871694909417909355600284018054600160ff1b1916600117905560038301805460ff1916905560048201805460ff191690555050508091505092915050565b5f805f808060015f8781526020019081526020015f2080546001600160a01b0316955060018201546001600160a01b03169450600282015460ff169350600382015460ff169250600482015460ff1691505091939550919395565b60015f8581526020019081526020015f206004015460ff161561027e576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610275906108f9565b60405180910390fd5b60015f8581526020019081526020015f206002015460ff168260ff16146102c8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016102bf9061093c565b60405180910390fd5b60015f8581526020019081526020015f2084600381106102e7576102e661095a565b5b60030284600381106102fb576102fa61095a565b5b01600501015460ff1615610332576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161032990610986565b60405180910390fd5b8160015f8681526020019081526020015f2084600381106103525761035161095a565b5b60030284600381106103665761036561095a565b5b0160050101805460ff191660ff83161790555060015f8581526020019081526020015f206003018054600101905561039f84826103b4565b1561042c578160015f8681526020019081526020015f20600401805460ff191660ff83161790555060015f8581526020019081526020015f20600201805460ff19169055610497565b600960015f8681526020019081526020015f206003015460ff16141561046857600360015f8681526020019081526020015f20600401805460ff191660031790555060015f8581526020019081526020015f20600201805460ff19169055610496565b60018260ff161461047c57600161047f565b60025b60015f8681526020019081526020015f20600201805460ff191660ff8316179055505b5b50505050565b5f5f5b600381101561044a5760015f8581526020019081526020015f2082600381106103df576103de61095a565b5b6003025f600381106103f2576103f161095a565b5b01600501015460ff168460ff16148015610419575060015f8581526020019081526020015f2082600381106104235761042261095a565b5b6003026001600381106104375761043661095a565b5b01600501015460ff168460ff16145b801561045e575060015f8581526020019081526020015f2082600381106104685761046761095a565b5b60030260026003811061047c5761047b61095a565b5b01600501015460ff168460ff16145b1561043c576001915050610418565b5b80806001019150506103b7565b505f5b60038110156104dc5760015f8581526020019081526020015f205f600381106104715761047061095a565b5b60030283600381106104855761048461095a565b5b01600501015460ff168460ff161480156104ab575060015f8581526020019081526020015f206001600381106104b5576104b461095a565b5b60030283600381106104c9576104c861095a565b5b01600501015460ff168460ff16145b80156104ef575060015f8581526020019081526020015f206002600381106104f9576104f861095a565b5b600302836003811061050d5761050c61095a565b5b01600501015460ff168460ff16145b156104ce576001915050610418565b5b8080600101915050610449565b5060015f8481526020019081526020015f205f600381106104fa576104f961095a565b5b6003025f6003811061050e5761050d61095a565b5b01600501015460ff168360ff16148015610534575060015f8481526020019081526020015f2060016003811061053e5761053d61095a565b5b6003026001600381106105525761055161095a565b5b01600501015460ff168360ff16145b8015610578575060015f8481526020019081526020015f206002600381106105825761058161095a565b5b6003026002600381106105965761059561095a565b5b01600501015460ff168360ff16145b1561059e5760019050610418565b60015f8481526020019081526020015f205f600381106105bc576105bb61095a565b5b6003026002600381106105d0576105cf61095a565b5b01600501015460ff168360ff161480156105f6575060015f8481526020019081526020015f20600160038110610600576105ff61095a565b5b6003026001600381106106145761061361095a565b5b01600501015460ff168360ff16145b801561063a575060015f8481526020019081526020015f206002600381106106445761064361095a565b5b6003025f600381106106585761065761095a565b5b01600501015460ff168360ff16145b156106605760019050610418565b5f90505b92915050565b5f80fd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6106968261066f565b9050919050565b6106a68161068c565b81146106b0575f80fd5b50565b5f813590506106c18161069d565b92915050565b5f604082840312156106db576106da61066b565b5b5f6106e8848285016106b3565b91505060206106f9848285016106b3565b90509250929050565b5f819050919050565b61071481610702565b811461071e575f80fd5b50565b5f8135905061072f8161070b565b92915050565b5f6020828403121561074d5761074c61066b565b5b5f61075a84828501610721565b91505092915050565b5f6020820190506107735f830184610702565b92915050565b5f81519050919050565b5f6060820190506107935f830186610779565b6107a06020830185610779565b6107ad6040830184610779565b949350505050565b5f6060820190506107c15f83018461077f565b92915050565b5f602082840312156107cc576107cb61066b565b5b5f6107d984828501610721565b91505092915050565b6107eb8161068c565b82525050565b60ff811682525050565b5f60a08201905061080b5f8301876107e2565b61081860208301866107e2565b61082560408301856107f1565b61083260608301846107f1565b61083f60808301836107f1565b95945050505050565b5f60a0820190506108545f8301876107e2565b61086160208301866107e2565b61086e60408301856107f1565b61087b60608301846107f1565b61088860808301836107f1565b95945050505050565b5f6020828403121561089b5761089a61066b565b5b5f6108a884828501610721565b91505092915050565b5f819050919050565b6108ca816108b8565b81146108d4575f80fd5b50565b5f813590506108e5816108c1565b92915050565b5f608082840312156109035761090261066b565b5b5f61091084828501610721565b94506020610920858286016108d7565b93506040610930858286016108d7565b92506060610940858286016108d7565b91505092959194509250565b5f6020820190506109595f8301846107f1565b92915050565b7f47616d65206973206f76657200000000000000000000000000000000000000005f82015250565b5f61099b600d83610947565b91506109a68261096b565b602082019050919050565b5f6020820190508181035f8301526109c88161098f565b9050919050565b7f4e6f7420796f7572207475726e000000000000000000000000000000000000005f82015250565b5f6109fc600e83610947565b9150610a07826109cc565b602082019050919050565b5f6020820190508181035f830152610a29816109f0565b9050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52603260045260245ffd5b7f43656c6c20616c72656164792074616b656e00000000000000000000000000005f82015250565b5f610a8b601283610947565b9150610a9682610a5b565b602082019050919050565b5f6020820190508181035f830152610ab881610a7f565b905091905056fea26469706673582212204561845f56ba8aa4fb5ef22201b17b2b64d48a3f8fa7bf7e5c9b68e9f50aaee764736f6c63430008230033"

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
