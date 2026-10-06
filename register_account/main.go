package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// ============================================================================
// METANODE ACCOUNT GATE REGISTRATION TOOL (GOLANG)
// Onboarding ví mới hoàn toàn vào chain con (Execution Cluster) qua Account Gate
// ============================================================================

type RPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	Id      int           `json:"id"`
}

type RPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Id      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ClusterIdentity struct {
	ClusterKey  string `json:"clusterKey"`
	ChainID     uint64 `json:"chainId"`
	AccountGate bool   `json:"accountGate"`
	MessageTag  string `json:"messageTag"`
}

type RegistrationMessage struct {
	Digest     string `json:"digest"`
	HashToSign string `json:"hashToSign"`
}

type RegistrationInfo struct {
	Status      string `json:"status"` // NONE | PENDING | CONFIRMED | REJECTED | FAILED
	HomeCluster string `json:"homeCluster,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type RegisteredAccountRecord struct {
	Index        int    `json:"index"`
	Address      string `json:"address"`
	PrivateKey   string `json:"private_key"`
	Status       string `json:"status"`
	RegisterTime string `json:"register_time"`
}

// Client gọi JSON-RPC
type NodeClient struct {
	rpcURL     string
	httpClient *http.Client
}

func NewNodeClient(rpcURL string) *NodeClient {
	return &NodeClient{
		rpcURL: strings.TrimRight(rpcURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *NodeClient) Call(method string, params []interface{}, resultHolder interface{}) error {
	reqBody, err := json.Marshal(RPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		Id:      int(time.Now().UnixNano() % 100000),
	})
	if err != nil {
		return fmt.Errorf("lỗi đóng gói request: %w", err)
	}

	resp, err := c.httpClient.Post(c.rpcURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("lỗi kết nối tới RPC (%s): %w", c.rpcURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("lỗi đọc response: %w", err)
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("lỗi parse JSON response: %w (raw: %s)", err, string(body))
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("RPC Error [Code %d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if resultHolder != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, resultHolder); err != nil {
			return fmt.Errorf("lỗi parse result data: %w", err)
		}
	}
	return nil
}

// Lấy thông tin Cluster Identity
func (c *NodeClient) GetClusterIdentity() (*ClusterIdentity, error) {
	var identity ClusterIdentity
	if err := c.Call("mtn_getClusterIdentity", []interface{}{}, &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

// Lấy message & hash cần ký cho địa chỉ ví
func (c *NodeClient) GetRegistrationMessage(addr common.Address) (*RegistrationMessage, error) {
	var msg RegistrationMessage
	if err := c.Call("mtn_getRegistrationMessage", []interface{}{addr.Hex()}, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Gửi yêu cầu đăng ký
func (c *NodeClient) RegisterAccount(addr common.Address, sigBytes []byte) (*RegistrationInfo, error) {
	var info RegistrationInfo
	sigHex := hexutil.Encode(sigBytes)
	if err := c.Call("mtn_registerAccount", []interface{}{addr.Hex(), sigHex}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Lấy trạng thái đăng ký hiện tại
func (c *NodeClient) GetRegistrationStatus(addr common.Address) (*RegistrationInfo, error) {
	var info RegistrationInfo
	if err := c.Call("mtn_getRegistrationStatus", []interface{}{addr.Hex()}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Kiểm tra số dư ví
func (c *NodeClient) GetBalance(addr common.Address) (*big.Int, error) {
	var balanceHex string
	if err := c.Call("eth_getBalance", []interface{}{addr.Hex(), "latest"}, &balanceHex); err != nil {
		return big.NewInt(0), err
	}
	balance, ok := new(big.Int).SetString(strings.TrimPrefix(balanceHex, "0x"), 16)
	if !ok {
		return big.NewInt(0), nil
	}
	return balance, nil
}

// Thực hiện toàn bộ quy trình onboarding 1 ví
func registerOneWallet(client *NodeClient, privKey *ecdsa.PrivateKey, index int) (*RegisteredAccountRecord, error) {
	userAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🚀 [%d] Đang xử lý ví: %s\n", index, userAddr.Hex())
	fmt.Printf("   Private Key: %s\n", privKeyHex)

	// 1. Kiểm tra trạng thái hiện tại của ví
	statusInfo, err := client.GetRegistrationStatus(userAddr)
	if err == nil && statusInfo != nil {
		if statusInfo.Status == "CONFIRMED" {
			fmt.Printf("   ✅ Ví này ĐÃ ĐƯỢC ĐĂNG KÝ từ trước (Status: CONFIRMED)!\n")
			return &RegisteredAccountRecord{
				Index:        index,
				Address:      userAddr.Hex(),
				PrivateKey:   privKeyHex,
				Status:       "CONFIRMED",
				RegisterTime: time.Now().Format(time.RFC3339),
			}, nil
		}
	}

	// 2. Lấy thông điệp cần ký
	fmt.Printf("   1️⃣ Đang lấy thông điệp đăng ký từ node (mtn_getRegistrationMessage)...\n")
	regMsg, err := client.GetRegistrationMessage(userAddr)
	if err != nil {
		return nil, fmt.Errorf("thất bại khi lấy thông điệp đăng ký: %w", err)
	}
	fmt.Printf("      • Digest:       %s\n", regMsg.Digest)
	fmt.Printf("      • Hash to Sign: %s\n", regMsg.HashToSign)

	// 3. Ký ECDSA secp256k1 lên hashToSign
	hashBytes, err := hexutil.Decode(regMsg.HashToSign)
	if err != nil {
		return nil, fmt.Errorf("lỗi decode hashToSign: %w", err)
	}

	sigBytes, err := crypto.Sign(hashBytes, privKey)
	if err != nil {
		return nil, fmt.Errorf("lỗi tạo chữ ký ECDSA: %w", err)
	}
	fmt.Printf("   2️⃣ Đã ký chữ ký ECDSA thành công: %s\n", hexutil.Encode(sigBytes))

	// 4. Gửi yêu cầu đăng ký lên Node thực thi
	fmt.Printf("   3️⃣ Đang gửi đăng ký lên Node thực thi (mtn_registerAccount)...\n")
	submitInfo, err := client.RegisterAccount(userAddr, sigBytes)
	if err != nil {
		return nil, fmt.Errorf("node từ chối yêu cầu đăng ký: %w", err)
	}
	fmt.Printf("      • Trạng thái phản hồi: %s (Reason: %s)\n", submitInfo.Status, submitInfo.Reason)

	// 5. Polling thăm dò trạng thái cho tới khi CONFIRMED
	fmt.Printf("   4️⃣ Đang chờ Node & Parent Chain xác nhận (Polling mtn_getRegistrationStatus)...\n")
	timeout := time.After(45 * time.Second)
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout sau 45s: ví chưa được chuyển sang trạng thái CONFIRMED")
		case <-ticker.C:
			currentStatus, err := client.GetRegistrationStatus(userAddr)
			if err != nil {
				fmt.Printf("      ⏳ Đang thăm dò... (lỗi nhẹ: %v)\n", err)
				continue
			}

			fmt.Printf("      ⏳ Trạng thái hiện tại: %s\n", currentStatus.Status)

			switch currentStatus.Status {
			case "CONFIRMED":
				fmt.Printf("   🎉 CHÚC MỪNG! Ví %s đã được Parent Chain & Node xác nhận THÀNH CÔNG!\n", userAddr.Hex())
				return &RegisteredAccountRecord{
					Index:        index,
					Address:      userAddr.Hex(),
					PrivateKey:   privKeyHex,
					Status:       "CONFIRMED",
					RegisterTime: time.Now().Format(time.RFC3339),
				}, nil

			case "REJECTED":
				return nil, fmt.Errorf("ví bị Parent Chain từ chối (REJECTED): đã đăng ký ở cụm khác (%s)", currentStatus.HomeCluster)

			case "FAILED":
				return nil, fmt.Errorf("đăng ký thất bại (FAILED): %s", currentStatus.Reason)

			case "PENDING":
				// Tiếp tục chờ
			default:
				// Trạng thái khác, tiếp tục chờ
			}
		}
	}
}

func main() {
	rpcFlag := flag.String("rpc", "http://127.0.0.1:8747", "http://192.168.1.234:8747)")
	countFlag := flag.Int("count", 1, "Số lượng ví mới cần tạo và đăng ký")
	walletPkFlag := flag.String("wallet-pk", "", "Private key hex của ví có sẵn (nếu muốn đăng ký cho ví cũ)")
	outFileFlag := flag.String("out", "registered_wallets.json", "Đường dẫn file JSON để xuất danh sách ví đã đăng ký")
	checkOnlyFlag := flag.String("check", "", "Địa chỉ ví cần kiểm tra trạng thái đăng ký (chỉ kiểm tra, không đăng ký)")
	flag.Parse()

	client := NewNodeClient(*rpcFlag)

	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║      METANODE ACCOUNT GATE ONBOARDING TOOL (SECP CHAIN)          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Printf("📡 Endpoint RPC: %s\n", *rpcFlag)

	// Kiểm tra kết nối và Cluster Identity
	clusterId, err := client.GetClusterIdentity()
	if err != nil {
		fmt.Printf("⚠️ Cảnh báo: Không thể lấy cluster identity từ RPC: %v\n", err)
		fmt.Printf("👉 Đảm bảo node đang chạy với `account_gate = parent_registered` và endpoint mtn_* đã bật.\n\n")
	} else {
		fmt.Printf("✅ Đã kết nối cụm thành công!\n")
		fmt.Printf("   • Chain ID:      %d\n", clusterId.ChainID)
		fmt.Printf("   • Cluster Key:   %s\n", clusterId.ClusterKey)
		fmt.Printf("   • Account Gate:  %v\n", clusterId.AccountGate)
		fmt.Printf("   • Message Tag:   %s\n\n", clusterId.MessageTag)
	}

	// Nếu chỉ kiểm tra 1 địa chỉ
	if *checkOnlyFlag != "" {
		checkAddr := common.HexToAddress(*checkOnlyFlag)
		fmt.Printf("🔍 Đang kiểm tra trạng thái ví: %s\n", checkAddr.Hex())
		status, err := client.GetRegistrationStatus(checkAddr)
		if err != nil {
			fmt.Printf("❌ Lỗi: %v\n", err)
			return
		}
		balance, _ := client.GetBalance(checkAddr)
		fmt.Printf("   • Trạng thái Gate: %s\n", status.Status)
		if status.HomeCluster != "" {
			fmt.Printf("   • Home Cluster:    %s\n", status.HomeCluster)
		}
		fmt.Printf("   • Số dư:           %s wei\n", balance.String())
		return
	}

	var results []*RegisteredAccountRecord

	// Trường hợp 1: Đăng ký cho 1 ví cụ thể qua private key
	if *walletPkFlag != "" {
		cleanPk := strings.TrimPrefix(*walletPkFlag, "0x")
		privKey, err := crypto.HexToECDSA(cleanPk)
		if err != nil {
			fmt.Printf("❌ Private key không hợp lệ: %v\n", err)
			os.Exit(1)
		}

		record, err := registerOneWallet(client, privKey, 1)
		if err != nil {
			fmt.Printf("❌ Lỗi đăng ký: %v\n", err)
			os.Exit(1)
		}
		results = append(results, record)

	} else {
		// Trường hợp 2: Sinh ngẫu nhiên N ví mới và đăng ký hàng loạt
		fmt.Printf("🔑 Chuẩn bị sinh ngẫu nhiên và đăng ký %d ví mới...\n", *countFlag)

		for i := 1; i <= *countFlag; i++ {
			privKey, err := crypto.GenerateKey()
			if err != nil {
				fmt.Printf("❌ Lỗi sinh private key cho ví %d: %v\n", i, err)
				continue
			}

			record, err := registerOneWallet(client, privKey, i)
			if err != nil {
				fmt.Printf("❌ Ví %d thất bại: %v\n", i, err)
			} else {
				results = append(results, record)
			}
		}
	}

	// Xuất kết quả ra file JSON
	if len(results) > 0 && *outFileFlag != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err == nil {
			_ = os.WriteFile(*outFileFlag, data, 0644)
			fmt.Printf("\n💾 Đã lưu danh sách %d ví đăng ký thành công vào file: %s\n", len(results), *outFileFlag)
		}
	}

	fmt.Printf("\n✨ Hoàn thành! (%d/%d ví đăng ký thành công)\n", len(results), *countFlag)
}
