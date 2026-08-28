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

const CaroBytecodeHex = "608060405260018055348015610013575f80fd5b50610dbe806100215f395ff3fe608060405260043610610049575f3560e01c8063117a5b901461004d5780632dc77c70146100cb57806345e09e54146100fa578063a6f979ff14610126578063b135bbb014610147575b5f80fd5b348015610058575f80fd5b506100b0610067366004610b40565b5f6020819052908152604090208054600182015460028301546006909301546001600160a01b0392831693919092169160ff808216916101008104821691620100009091041686565b6040516100c296959493929190610b8f565b60405180910390f35b3480156100d6575f80fd5b506100ea6100e5366004610bf1565b61015c565b60405190151581526020016100c2565b348015610105575f80fd5b50610119610114366004610b40565b6105b6565b6040516100c29190610c3b565b610139610134366004610cb4565b6106a2565b6040519081526020016100c2565b348015610152575f80fd5b5061013960015481565b5f848152602081905260408120816006820154610100900460ff16600381111561018857610188610b57565b146101cc5760405162461bcd60e51b815260206004820152600f60248201526e47616d65206e6f742061637469766560881b60448201526064015b60405180910390fd5b60038560ff161080156101e2575060038460ff16105b6102245760405162461bcd60e51b8152602060048201526013602482015272496e76616c696420636f6f7264696e6174657360681b60448201526064016101c3565b5f816003018660ff166003811061023d5761023d610ce5565b018560ff166003811061025257610252610ce5565b602081049091015460ff601f9092166101000a900416600281111561027957610279610b57565b146102be5760405162461bcd60e51b815260206004820152601560248201527410d95b1b08185b1c9958591e481bd8d8dd5c1a5959605a1b60448201526064016101c3565b5f8360ff1660028111156102d4576102d4610b57565b600683015490915060ff1660028111156102f0576102f0610b57565b81600281111561030257610302610b57565b1461033f5760405162461bcd60e51b815260206004820152600d60248201526c2737ba103cb7bab9103a3ab93760991b60448201526064016101c3565b80826003018760ff166003811061035857610358610ce5565b018660ff166003811061036d5761036d610ce5565b602091828204019190066101000a81548160ff0219169083600281111561039657610396610b57565b021790555060068201805462010000900460ff169060026103b683610d0d565b91906101000a81548160ff021916908360ff16021790555050867f186abf997d1d690189496edbd4fb7615ab41008858e22b21c45e4175e8fa8ca287878460405161040393929190610d2b565b60405180910390a2610415878261075c565b156104f457600181600281111561042e5761042e610b57565b1461043a57600261043d565b60015b60068301805461ff00191661010083600381111561045d5761045d610b57565b02179055505f600182600281111561047757610477610b57565b1461048f5760018301546001600160a01b031661049b565b82546001600160a01b03165b9050877f69137706d65cad5bb2a9a944e02d6c3474190ffcaf7c1eb0e70474b5a52ecc628460060160019054906101000a900460ff16836040516104e0929190610d4a565b60405180910390a2600193505050506105ae565b600682015462010000900460ff166009036105625760068201805461ff00191661030017905560405187907f69137706d65cad5bb2a9a944e02d6c3474190ffcaf7c1eb0e70474b5a52ecc629061054f906003905f90610d4a565b60405180910390a26001925050506105ae565b600181600281111561057657610576610b57565b14610582576001610585565b60025b60068301805460ff191660018360028111156105a3576105a3610b57565b02179055505f925050505b949350505050565b6105be610af5565b5f828152602081905260408120905b60038160ff16101561069b575f5b60038160ff16101561068857826003018260ff16600381106105ff576105ff610ce5565b018160ff166003811061061457610614610ce5565b602081049091015460ff601f9092166101000a900416600281111561063b5761063b610b57565b848360ff166003811061065057610650610ce5565b60200201518260ff166003811061066957610669610ce5565b60ff90921660209290920201528061068081610d0d565b9150506105db565b508061069381610d0d565b9150506105cd565b5050919050565b600180545f91829190826106b583610d70565b909155505f818152602081815260409182902080546001600160a01b038981166001600160a01b03199283168117845560018085018054938c16939094168317909355346002850181905560068501805461ffff1916909417909355855190815293840152828401529151929350909183917f6200407c0ea392b8107b21a9be480acd41fda186d04bed28cc7da2d4b53d56e2919081900360600190a25090505b92915050565b5f828152602081905260408120815b60038160ff16101561098f5783600281111561078957610789610b57565b826003018260ff16600381106107a1576107a1610ce5565b015460ff1660028111156107b7576107b7610b57565b14801561080657508360028111156107d1576107d1610b57565b826003018260ff16600381106107e9576107e9610ce5565b0154610100900460ff16600281111561080457610804610b57565b145b8015610855575083600281111561081f5761081f610b57565b826003018260ff166003811061083757610837610ce5565b015462010000900460ff16600281111561085357610853610b57565b145b1561086557600192505050610756565b83600281111561087757610877610b57565b600383015f018260ff166003811061089157610891610ce5565b602081049091015460ff601f9092166101000a90041660028111156108b8576108b8610b57565b14801561091357508360028111156108d2576108d2610b57565b6004830160ff8316600381106108ea576108ea610ce5565b602081049091015460ff601f9092166101000a900416600281111561091157610911610b57565b145b801561096d575083600281111561092c5761092c610b57565b6005830160ff83166003811061094457610944610ce5565b602081049091015460ff601f9092166101000a900416600281111561096b5761096b610b57565b145b1561097d57600192505050610756565b8061098781610d0d565b91505061076b565b508260028111156109a2576109a2610b57565b600382015460ff1660028111156109bb576109bb610b57565b1480156109f557508260028111156109d5576109d5610b57565b6004820154610100900460ff1660028111156109f3576109f3610b57565b145b8015610a2f5750826002811115610a0e57610a0e610b57565b600582015462010000900460ff166002811115610a2d57610a2d610b57565b145b15610a3e576001915050610756565b826002811115610a5057610a50610b57565b600382015462010000900460ff166002811115610a6f57610a6f610b57565b148015610aa95750826002811115610a8957610a89610b57565b6004820154610100900460ff166002811115610aa757610aa7610b57565b145b8015610add5750826002811115610ac257610ac2610b57565b600582015460ff166002811115610adb57610adb610b57565b145b15610aec576001915050610756565b505f9392505050565b60405180606001604052806003905b610b0c610b22565b815260200190600190039081610b045790505090565b60405180606001604052806003906020820280368337509192915050565b5f60208284031215610b50575f80fd5b5035919050565b634e487b7160e01b5f52602160045260245ffd5b60038110610b7b57610b7b610b57565b9052565b60048110610b7b57610b7b610b57565b6001600160a01b038781168252861660208201526040810185905260c08101610bbb6060830186610b6b565b610bc86080830185610b7f565b60ff831660a0830152979650505050505050565b803560ff81168114610bec575f80fd5b919050565b5f805f8060808587031215610c04575f80fd5b84359350610c1460208601610bdc565b9250610c2260408601610bdc565b9150610c3060608601610bdc565b905092959194509250565b610120810181835f805b6003808210610c545750610c94565b835185845b83811015610c7a57825160ff16825260209283019290910190600101610c59565b505050606094909401935060209290920191600101610c45565b5050505092915050565b80356001600160a01b0381168114610bec575f80fd5b5f8060408385031215610cc5575f80fd5b610cce83610c9e565b9150610cdc60208401610c9e565b90509250929050565b634e487b7160e01b5f52603260045260245ffd5b634e487b7160e01b5f52601160045260245ffd5b5f60ff821660ff8103610d2257610d22610cf9565b60010192915050565b60ff848116825283166020820152606081016105ae6040830184610b6b565b60408101610d588285610b7f565b6001600160a01b039290921660209190910152919050565b5f60018201610d8157610d81610cf9565b506001019056fea2646970667358221220c0f3f840faa9c3b1024a091b52ade82f774d8d5d3798f99de1a45f17a019e1ef64736f6c63430008140033"

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

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) *types.Receipt {
	start := time.Now()
	for time.Since(start) < timeout {
		res, err := callRPC(url, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			var r types.Receipt
			if json.Unmarshal(res, &r) == nil {
				return &r
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return nil
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
	var hexResult string
	if err := json.Unmarshal(res, &hexResult); err != nil {
		return "", err
	}
	return hexResult, nil
}

// ─── BOARD PRINTER ───────────────────────────────────────────────────────────
type CaroBoard struct {
	Grid      [3][3]int
	MoveCount int
	Status    int // 0: InProgress, 1: X_Won, 2: O_Won, 3: Draw
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

func main() {
	interactiveFlag := flag.Bool("interactive", false, "Chế độ người chơi tự tay nhập nước đi từ bàn phím")
	flag.Parse()

	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "🎮 GAME CARO XUYÊN CHUỖI PURE CLIENT (EVM GENERAL MESSAGE PASSING)          " + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)

	rpcA := "http://127.0.0.1:8546" // Chain 101
	rpcB := "http://127.0.0.1:8547" // Chain 102

	// Tự động tải endpoint RPC từ config.json của test-blockstm hoặc /tmp/private_chains.json
	for _, cfgPath := range []string{"../config.json", "../../config.json", "/tmp/private_chains.json"} {
		if data, err := os.ReadFile(cfgPath); err == nil {
			var bCfg struct {
				PrivateChains struct {
					ChainA struct {
						RpcUrl string `json:"rpc_url"`
					} `json:"chain_a"`
					ChainB struct {
						RpcUrl string `json:"rpc_url"`
					} `json:"chain_b"`
				} `json:"private_chains"`
				Nodes map[string]string `json:"nodes"`
			}
			if err := json.Unmarshal(data, &bCfg); err == nil {
				if rpcA == "http://127.0.0.1:8546" {
					if bCfg.PrivateChains.ChainA.RpcUrl != "" {
						rpcA = bCfg.PrivateChains.ChainA.RpcUrl
					} else if bCfg.Nodes["101"] != "" {
						rpcA = bCfg.Nodes["101"]
					}
				}
				if rpcB == "http://127.0.0.1:8547" {
					if bCfg.PrivateChains.ChainB.RpcUrl != "" {
						rpcB = bCfg.PrivateChains.ChainB.RpcUrl
					} else if bCfg.Nodes["102"] != "" {
						rpcB = bCfg.Nodes["102"]
					}
				}
			}
		}
	}

	keyA := "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
	keyB := "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a"

	privKey1, _ := crypto.HexToECDSA(keyA)
	player1Addr := crypto.PubkeyToAddress(privKey1.PublicKey)

	privKey2, _ := crypto.HexToECDSA(keyB)
	player2Addr := crypto.PubkeyToAddress(privKey2.PublicKey)

	parsedGatewayABI, _ := abi.JSON(strings.NewReader(GatewayABI))
	parsedCaroABI, _ := abi.JSON(strings.NewReader(CaroContractABI))

	fmt.Printf("👥 NGƯỜI CHƠI:\n")
	fmt.Printf("   ├─ %sNgười chơi X%s (Chain 101): %s\n", ColorRed+ColorBold, ColorReset, player1Addr.Hex())
	fmt.Printf("   └─ %sNgười chơi O%s (Chain 102): %s\n", ColorCyan+ColorBold, ColorReset, player2Addr.Hex())

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 1: DEPLOY SMART CONTRACT CARO TRÊN CHAIN 102 (DESTINATION CHAIN)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "📜 BƯỚC 1: KHỞI TẠO BÀN CỜ CARO TRÊN CHAIN 102" + ColorReset)
	caroBytecode, _ := hexutil.Decode("0x" + CaroBytecodeHex)
	nonceDeploy, _ := getNonce(rpcB, player2Addr.Hex())

	deployTx := types.NewContractCreation(nonceDeploy, big.NewInt(0), 4_000_000, big.NewInt(1e9), caroBytecode)
	signedDeployTx, _ := types.SignTx(deployTx, types.NewEIP155Signer(big.NewInt(102)), privKey2)
	rawDeployBytes, _ := signedDeployTx.MarshalBinary()

	deployHash, errDeploy := sendRawTransaction(rpcB, rawDeployBytes)
	if errDeploy != nil {
		fmt.Printf("❌ Lỗi deploy Caro contract: %v\n", errDeploy)
		return
	}
	waitForReceipt(rpcB, deployHash, 10*time.Second)
	caroContractAddr := crypto.CreateAddress(player2Addr, nonceDeploy)
	fmt.Printf("   ✅ Smart Contract Caro Address trên Chain 102: %s%s%s\n", ColorPurple, caroContractAddr.Hex(), ColorReset)

	// Khởi tạo Game #1 trên Smart Contract
	createGameData, _ := parsedCaroABI.Pack("createGame", player1Addr, player2Addr)
	time.Sleep(1 * time.Second)
	nonceCreate, _ := getNonce(rpcB, player2Addr.Hex())
	txCreate := types.NewTransaction(nonceCreate, caroContractAddr, big.NewInt(0), 500000, big.NewInt(1e9), createGameData)
	signedCreate, _ := types.SignTx(txCreate, types.NewEIP155Signer(big.NewInt(102)), privKey2)
	rawCreate, _ := signedCreate.MarshalBinary()
	hashCreate, _ := sendRawTransaction(rpcB, rawCreate)
	waitForReceipt(rpcB, hashCreate, 10*time.Second)
	fmt.Printf("   ✅ Game ID #1 đã được tạo trên Smart Contract!\n")

	board := &CaroBoard{}
	board.PrintBoard("BÀN CỜ BAN ĐẦU (0/9 NƯỚC ĐI)")

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 2: TIẾN TRÌNH THI ĐẤU XUYÊN CHUỖI (PURE CLIENT)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "⚔️ BƯỚC 2: TIẾN TRÌNH THI ĐẤU (CLIENT CHỈ GỬI - RELAYER TỰ ĐỘNG CHUYỂN TIẾP)" + ColorReset)

	scriptedMoves := [][2]int{
		{1, 1}, // X (Chain 101): Tâm
		{0, 1}, // O (Chain 102): Trên
		{0, 0}, // X (Chain 101): Góc trái trên
		{2, 1}, // O (Chain 102): Dưới
		{2, 2}, // X (Chain 101): Góc phải dưới ➔ X Thắng chéo (0,0)-(1,1)-(2,2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	turnCount := 1
	currentTurnCell := 1 // 1: X (Chain 101), 2: O (Chain 102)

	for board.Status == 0 {
		var row, col int

		if currentTurnCell == 1 {
			// LƯỢT CỦA PLAYER X (CHAIN 101 ➔ GỬI OUTBOUND SANG CHAIN 102)
			if *interactiveFlag {
				for {
					fmt.Printf("\n👉 %sLƯỢT CỦA BẠN (QUÂN [ X ] TRÊN CHAIN 101):%s Nhập 'hàng cột' (0-2), ví dụ '1 1': ", ColorRed+ColorBold, ColorReset)
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

			fmt.Printf("\n▶ %sNƯỚC %d: Người chơi X (Chain 101) ĐÁNH Ô (%d, %d)%s\n", ColorBold, turnCount, row, col, ColorReset)

			// 1. Pack playMove payload
			playMovePayload, _ := parsedCaroABI.Pack("playMove", big.NewInt(1), uint8(row), uint8(col), uint8(1))

			// 2. Client gọi outbound trên Gateway Chain 101
			tipAmount := big.NewInt(1e16)                   // 0.01 MTN Tip
			gasFeeAmount := big.NewInt(100_000_000_000_000) // 0.0001 MTN Gas Fee
			totalBurn := new(big.Int).Add(tipAmount, gasFeeAmount)

			outboundData, _ := parsedGatewayABI.Pack("outbound", big.NewInt(102), caroContractAddr, playMovePayload, big.NewInt(0), big.NewInt(0), tipAmount, gasFeeAmount, uint8(1), false)
			time.Sleep(500 * time.Millisecond)
			nonceA, _ := getNonce(rpcA, player1Addr.Hex())

			txOutbound := types.NewTransaction(nonceA, GatewayAddress, totalBurn, 500000, big.NewInt(1e9), outboundData)
			signedTxOutbound, _ := types.SignTx(txOutbound, types.NewEIP155Signer(big.NewInt(101)), privKey1)
			rawTxBytes, _ := signedTxOutbound.MarshalBinary()

			txHashOutbound, errSend := sendRawTransaction(rpcA, rawTxBytes)
			if errSend != nil {
				fmt.Printf("   ❌ Lỗi gửi outbound trên Chain 101: %v\n", errSend)
				return
			}
			fmt.Printf("   🚀 Lệnh nộp lên Gateway Chain 101 (Tx: %s) ✅\n", txHashOutbound.Hex())
			fmt.Printf("   ⏳ Client đứng đợi Relayer Daemon ngầm chuyển tiếp sang Chain 102...\n")

			// 3. Client polling Smart Contract trên Chain 102 để xem nước đi đã được thực thi chưa
			synced := false
			for attempt := 0; attempt < 60; attempt++ {
				time.Sleep(1 * time.Second)
				getBoardData, _ := parsedCaroABI.Pack("getBoard", big.NewInt(1))
				resHex, errCall := ethCall(rpcB, caroContractAddr, getBoardData)
				if errCall == nil && resHex != "" && resHex != "0x" {
					resBytes := common.FromHex(resHex)
					out, unpackErr := parsedCaroABI.Unpack("getBoard", resBytes)
					if unpackErr == nil && len(out) > 0 {
						gridArr := out[0].([3][3]uint8)
						if gridArr[row][col] == 1 {
							synced = true
							board.Grid[row][col] = 1
							fmt.Printf("   %s🎉 RELAYER ĐÃ CHUYỂN THÀNH CÔNG! Smart Contract Chain 102 đã nhận nước đi (%d, %d)!%s\n", ColorGreen, row, col, ColorReset)
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
			// LƯỢT CỦA PLAYER O (CHAIN 102)
			scriptIdx := turnCount - 1
			if scriptIdx < len(scriptedMoves) {
				row = scriptedMoves[scriptIdx][0]
				col = scriptedMoves[scriptIdx][1]
			} else {
				row, col = board.FindBestAIMove()
			}

			fmt.Printf("\n▶ %sNƯỚC %d: Người chơi O (Chain 102) ĐÁNH Ô (%d, %d)%s\n", ColorBold, turnCount, row, col, ColorReset)

			// Player O gọi trực tiếp Smart Contract trên Chain 102
			playMoveData, _ := parsedCaroABI.Pack("playMove", big.NewInt(1), uint8(row), uint8(col), uint8(2))
			time.Sleep(500 * time.Millisecond)
			nonceB, _ := getNonce(rpcB, player2Addr.Hex())

			txMoveB := types.NewTransaction(nonceB, caroContractAddr, big.NewInt(0), 500000, big.NewInt(1e9), playMoveData)
			signedTxB, _ := types.SignTx(txMoveB, types.NewEIP155Signer(big.NewInt(102)), privKey2)
			rawTxB, _ := signedTxB.MarshalBinary()

			txHashB, _ := sendRawTransaction(rpcB, rawTxB)
			waitForReceipt(rpcB, txHashB, 10*time.Second)
			board.Grid[row][col] = 2
			fmt.Printf("   ✅ Đã đánh trực tiếp trên Chain 102 (Tx: %s)!\n", txHashB.Hex())
		}

		// Kiểm tra trạng thái Game trên Smart Contract
		gamesData, _ := parsedCaroABI.Pack("games", big.NewInt(1))
		gamesResHex, _ := ethCall(rpcB, caroContractAddr, gamesData)
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
			fmt.Printf("\n🎉 %sCHIẾN THẮNG TUYỆT ĐỐI! Người chơi X (Chain 101) ĐÃ THẮNG TRÊN SMART CONTRACT CHAIN 102!%s\n", ColorGreen+ColorBold, ColorReset)
			break
		} else if board.Status == 2 {
			fmt.Printf("\n🎉 %sCHIẾN THẮNG! Người chơi O (Chain 102) ĐÃ THẮNG TRÊN SMART CONTRACT!%s\n", ColorGreen+ColorBold, ColorReset)
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
	fmt.Println(ColorGreen + ColorBold + "🏆 TRẬN ĐẤU CARO XUYÊN CHUỖI PURE CLIENT ĐÃ THỰC THI QUA RELAYER THÀNH CÔNG 100%!" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
}
