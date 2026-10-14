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

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	"tool-test/pkg/logger"
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
	Status      string `json:"status"` // NONE | PENDING | PROVISIONAL | CONFIRMED | REJECTED | FAILED
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
		if statusInfo.Status == "CONFIRMED" || statusInfo.Status == "PROVISIONAL" {
			fmt.Printf("   ✅ Ví này đã hoạt động trên cụm (Status: %s)!\n", statusInfo.Status)
			return &RegisteredAccountRecord{
				Index:        index,
				Address:      userAddr.Hex(),
				PrivateKey:   privKeyHex,
				Status:       statusInfo.Status,
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

			case "PROVISIONAL":
				fmt.Printf("   ✅ Ví %s đã được kích hoạt tạm trên cụm khi Parent Chain offline.\n", userAddr.Hex())
				return &RegisteredAccountRecord{
					Index:        index,
					Address:      userAddr.Hex(),
					PrivateKey:   privKeyHex,
					Status:       "PROVISIONAL",
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

// Thực hiện quy trình onboarding 1 ví qua TCP Socket với cơ chế ASYNC PUSH (Không Polling)
func registerOneWalletTCP(client *client_tcp.Client, privKey *ecdsa.PrivateKey, index int) (*RegisteredAccountRecord, error) {
	userAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🚀 [%d] (TCP Socket) Đang xử lý ví: %s\n", index, userAddr.Hex())
	fmt.Printf("   Private Key: %s\n", privKeyHex)

	// 1. Kiểm tra trạng thái hiện tại của ví qua TCP (GetRegistrationStatus)
	statusInfo, err := client.ChainGetRegistrationStatus(userAddr)
	if err == nil && statusInfo != nil {
		if statusInfo.Status == "CONFIRMED" || statusInfo.Status == "PROVISIONAL" {
			fmt.Printf("   ✅ Ví này đã hoạt động trên cụm (Status: %s)!\n", statusInfo.Status)
			return &RegisteredAccountRecord{
				Index:        index,
				Address:      userAddr.Hex(),
				PrivateKey:   privKeyHex,
				Status:       statusInfo.Status,
				RegisterTime: time.Now().Format(time.RFC3339),
			}, nil
		}
	}

	// 2. Lấy 32-byte hashToSign cần ký từ node qua TCP (GetRegistrationMessage)
	fmt.Printf("   1️⃣ Đang lấy thông điệp đăng ký từ node qua TCP (GetRegistrationMessage)...\n")
	hashToSign, err := client.ChainGetRegistrationMessage(userAddr)
	if err != nil {
		return nil, fmt.Errorf("thất bại khi lấy thông điệp đăng ký qua TCP: %w", err)
	}
	fmt.Printf("      • Hash to Sign: %s\n", hashToSign.Hex())

	// 3. Ký ECDSA secp256k1 lên hashToSign
	sigBytes, err := crypto.Sign(hashToSign.Bytes(), privKey)
	if err != nil {
		return nil, fmt.Errorf("lỗi tạo chữ ký ECDSA: %w", err)
	}
	fmt.Printf("   2️⃣ Đã ký chữ ký ECDSA thành công: %s\n", hexutil.Encode(sigBytes))

	// 4. Gửi yêu cầu đăng ký và CHỜ ASYNC PUSH (KHÔNG POLLING)
	fmt.Printf("   3️⃣ Gửi đăng ký lên TCP Socket & chờ kết quả Push (ChainRegisterAccountWait - KHÔNG Polling)...\n")
	result, err := client.ChainRegisterAccountWait(userAddr, sigBytes, 45*time.Second)
	if err != nil {
		return nil, fmt.Errorf("đăng ký qua TCP thất bại: %w", err)
	}

	fmt.Printf("      • Kết quả Push nhận được: %s (Reason: %s)\n", result.Status, result.Reason)

	switch result.Status {
	case "CONFIRMED":
		fmt.Printf("   🎉 CHÚC MỪNG! Node chủ động PUSH kết quả CONFIRMED qua TCP Socket THÀNH CÔNG!\n")
		return &RegisteredAccountRecord{
			Index:        index,
			Address:      userAddr.Hex(),
			PrivateKey:   privKeyHex,
			Status:       "CONFIRMED",
			RegisterTime: time.Now().Format(time.RFC3339),
		}, nil

	case "PROVISIONAL":
		fmt.Printf("   ✅ Node PUSH kết quả PROVISIONAL; ví được phép giao dịch nội bộ và chưa được rút cross-chain.\n")
		return &RegisteredAccountRecord{
			Index:        index,
			Address:      userAddr.Hex(),
			PrivateKey:   privKeyHex,
			Status:       "PROVISIONAL",
			RegisterTime: time.Now().Format(time.RFC3339),
		}, nil

	case "REJECTED":
		return nil, fmt.Errorf("ví bị Parent Chain từ chối (REJECTED): đã đăng ký ở cụm khác (%s)", result.HomeCluster)

	case "FAILED":
		return nil, fmt.Errorf("đăng ký thất bại (FAILED): %s", result.Reason)

	default:
		return nil, fmt.Errorf("trạng thái không xác định: %s", result.Status)
	}
}

func main() {
	rpcFlag := flag.String("rpc", "http://127.0.0.1:8747", "Endpoint JSON-RPC HTTP (ví dụ: http://192.168.1.234:8747)")
	tcpFlag := flag.String("tcp", "", "Endpoint TCP Socket của node hoặc proxy (ví dụ: 127.0.0.1:4200 hoặc 127.0.0.1:9545)")
	tcpConfigFlag := flag.String("tcp-config", "", "Đường dẫn file cấu hình JSON TCP (tùy chọn)")
	countFlag := flag.Int("count", 1, "Số lượng ví mới cần tạo và đăng ký")
	walletPkFlag := flag.String("wallet-pk", "", "Private key hex của ví có sẵn (nếu muốn đăng ký cho ví cũ)")
	outFileFlag := flag.String("out", "registered_wallets.json", "Đường dẫn file JSON để xuất danh sách ví đã đăng ký")
	checkOnlyFlag := flag.String("check", "", "Địa chỉ ví cần kiểm tra trạng thái đăng ký (chỉ kiểm tra, không đăng ký)")
	flag.Parse()

	// ══════════════════════════════════════════════════════════════════
	// CHẾ ĐỘ 1: THỰC THI QUA GIAO THỨC TCP SOCKET (NẾU CÓ CỜ -tcp HOẶC -tcp-config)
	// ══════════════════════════════════════════════════════════════════
	if *tcpFlag != "" || *tcpConfigFlag != "" {
		var cfg *tcp_config.ClientConfig
		if *tcpConfigFlag != "" {
			cfgRaw, err := tcp_config.LoadConfig(*tcpConfigFlag)
			if err != nil {
				fmt.Printf("❌ Lỗi đọc file config TCP: %v\n", err)
				os.Exit(1)
			}
			cfg = cfgRaw.(*tcp_config.ClientConfig)
		} else {
			cfg = &tcp_config.ClientConfig{
				ParentConnectionAddress: *tcpFlag,
				ParentConnectionType:    "client",
				Version_:                "0.0.1.0",
			}
		}
		if *tcpFlag != "" {
			cfg.ParentConnectionAddress = *tcpFlag
		}

		logger.SetConfig(&logger.LoggerConfig{
			Flag:    logger.FLAG_INFO,
			Outputs: []*os.File{os.Stdout},
		})

		fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
		fmt.Println("║      METANODE ACCOUNT GATE ONBOARDING TOOL (TCP SOCKET)          ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
		fmt.Printf("📡 Endpoint TCP: %s\n", cfg.GetParentConnectionAddress())

		tcpClient, err := client_tcp.NewClient(cfg)
		if err != nil {
			fmt.Printf("❌ Lỗi kết nối TCP %s: %v\n", cfg.GetParentConnectionAddress(), err)
			os.Exit(1)
		}
		time.Sleep(500 * time.Millisecond)

		// Chỉ kiểm tra 1 địa chỉ qua TCP
		if *checkOnlyFlag != "" {
			checkAddr := common.HexToAddress(*checkOnlyFlag)
			fmt.Printf("🔍 (TCP) Đang kiểm tra trạng thái ví: %s\n", checkAddr.Hex())
			status, err := tcpClient.ChainGetRegistrationStatus(checkAddr)
			if err != nil {
				fmt.Printf("❌ Lỗi kiểm tra status qua TCP: %v\n", err)
				return
			}
			fmt.Printf("   • Trạng thái Gate: %s\n", status.Status)
			if status.HomeCluster != "" {
				fmt.Printf("   • Home Cluster:    %s\n", status.HomeCluster)
			}
			if as, err := tcpClient.AccountState(checkAddr); err == nil && as != nil {
				fmt.Printf("   • Số dư:           %s wei\n", as.Balance().String())
				fmt.Printf("   • Nonce:           %d\n", as.Nonce())
			}
			return
		}

		var results []*RegisteredAccountRecord

		if *walletPkFlag != "" {
			cleanPk := strings.TrimPrefix(*walletPkFlag, "0x")
			privKey, err := crypto.HexToECDSA(cleanPk)
			if err != nil {
				fmt.Printf("❌ Private key không hợp lệ: %v\n", err)
				os.Exit(1)
			}
			record, err := registerOneWalletTCP(tcpClient, privKey, 1)
			if err != nil {
				fmt.Printf("❌ Lỗi đăng ký qua TCP: %v\n", err)
				os.Exit(1)
			}
			results = append(results, record)
		} else {
			fmt.Printf("🔑 Chuẩn bị sinh ngẫu nhiên và đăng ký %d ví mới qua TCP Socket (Push Mode)...\n", *countFlag)
			for i := 1; i <= *countFlag; i++ {
				privKey, err := crypto.GenerateKey()
				if err != nil {
					fmt.Printf("❌ Lỗi sinh private key cho ví %d: %v\n", i, err)
					continue
				}
				record, err := registerOneWalletTCP(tcpClient, privKey, i)
				if err != nil {
					fmt.Printf("❌ Ví %d thất bại: %v\n", i, err)
				} else {
					results = append(results, record)
				}
			}
		}

		if len(results) > 0 && *outFileFlag != "" {
			data, err := json.MarshalIndent(results, "", "  ")
			if err == nil {
				_ = os.WriteFile(*outFileFlag, data, 0644)
				fmt.Printf("\n💾 Đã lưu danh sách %d ví đăng ký thành công vào file: %s\n", len(results), *outFileFlag)
			}
		}

		fmt.Printf("\n✨ Hoàn thành qua TCP! (%d/%d ví đăng ký thành công)\n", len(results), *countFlag)
		return
	}

	// ══════════════════════════════════════════════════════════════════
	// CHẾ ĐỘ 2: THỰC THI QUA JSON-RPC HTTP (CHẾ ĐỘ MẶC ĐỊNH / POLLING)
	// ══════════════════════════════════════════════════════════════════
	client := NewNodeClient(*rpcFlag)

	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║      METANODE ACCOUNT GATE ONBOARDING TOOL (HTTP JSON-RPC)       ║")
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
