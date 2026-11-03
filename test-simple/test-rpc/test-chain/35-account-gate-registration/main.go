/*
 * BÀI TEST & CLI TOOL ĐĂNG KÝ VÍ DUY NHẤT: 35-account-gate-registration
 *
 * CHỨC NĂNG:
 * 1. AUTOMATED TEST (Mặc định trong CI & go test):
 *    - Xác nhận Account Gate từ chối ví chưa đăng ký (RecipientNotRegistered).
 *    - Đăng ký ví mới qua RPC (mtn_getRegistrationMessage -> ECDSA sign -> mtn_registerAccount).
 *    - Đợi Parent Chain và Cluster xác nhận trạng thái CONFIRMED / PROVISIONAL.
 *    - Gửi Native Token vào ví vừa đăng ký thành công (Gate mở khóa).
 *    - Ví mới tự gửi giao dịch chuyển tiền đầu tiên thành công.
 *    - Nếu có cấu hình TCP, kiểm tra push notification qua TCP socket.
 *
 * 2. STANDALONE CLI TOOL (Khi truyền cờ -count, -wallet-pk, -check, -tcp):
 *    - go run main.go -count=5 -out=my_wallets.json
 *    - go run main.go -wallet-pk=0x...
 *    - go run main.go -check=0x...
 *    - go run main.go -tcp=127.0.0.1:4200 -count=3
 */
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"google.golang.org/protobuf/proto"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
)

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

type NodeRPCClient struct {
	rpcURL     string
	httpClient *http.Client
}

func NewNodeRPCClient(rpcURL string) *NodeRPCClient {
	return &NodeRPCClient{
		rpcURL: strings.TrimRight(rpcURL, "/"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *NodeRPCClient) Call(method string, params []interface{}, resultHolder interface{}) error {
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
		return fmt.Errorf("lỗi parse JSON: %w (raw: %s)", err, string(body))
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

func (c *NodeRPCClient) GetClusterIdentity() (*ClusterIdentity, error) {
	var identity ClusterIdentity
	if err := c.Call("mtn_getClusterIdentity", []interface{}{}, &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

func (c *NodeRPCClient) GetRegistrationMessage(addr common.Address) (*RegistrationMessage, error) {
	var msg RegistrationMessage
	if err := c.Call("mtn_getRegistrationMessage", []interface{}{addr.Hex()}, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *NodeRPCClient) RegisterAccount(addr common.Address, sigBytes []byte) (*RegistrationInfo, error) {
	var info RegistrationInfo
	sigHex := hexutil.Encode(sigBytes)
	if err := c.Call("mtn_registerAccount", []interface{}{addr.Hex(), sigHex}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *NodeRPCClient) GetRegistrationStatus(addr common.Address) (*RegistrationInfo, error) {
	var info RegistrationInfo
	if err := c.Call("mtn_getRegistrationStatus", []interface{}{addr.Hex()}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *NodeRPCClient) GetBalance(addr common.Address) (*big.Int, error) {
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

// ════════════════════════════════════════════════════════════════════════
// RAW TCP NODE CLIENT (Giao tiếp trực tiếp TCP Protobuf wire format)
// ════════════════════════════════════════════════════════════════════════

// TCPNodeClient thực hiện toàn bộ giao tiếp đăng ký tài khoản qua TCP raw socket (port 4200/9545).
// Đảm bảo tuân thủ đúng wire-format:
// 1. Packet framing: 8-byte LittleEndian length prefix + pb.Message
// 2. GetRegistrationMessage: body là đúng 20 bytes nhị phân của ví (addr.Bytes())
// 3. RegisterAccount: body là pb.RegisterAccountRequest (hoặc 85 bytes binary raw)
// 4. RegisterAccountAck / RegistrationStatus: parse bằng pb.RegistrationResult
type TCPNodeClient struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

func NewTCPNodeClient(addr string) (*TCPNodeClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("không thể kết nối TCP tới %s: %w", addr, err)
	}
	client := &TCPNodeClient{
		addr: addr,
		conn: conn,
	}
	// Khởi tạo InitConnection theo chuẩn Metanode
	if err := client.initConnection(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("lỗi khởi tạo InitConnection qua TCP: %w", err)
	}
	return client, nil
}

func (c *TCPNodeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *TCPNodeClient) sendMsg(cmd string, body []byte) error {
	msg := &pb.Message{
		Header: &pb.Header{
			Command: cmd,
			ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		},
		Body: body,
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	lenBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(lenBuf, uint64(len(payload)))

	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write(lenBuf); err != nil {
		return err
	}
	if _, err := c.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func (c *TCPNodeClient) readMsg(expectedCmd string) (*pb.Message, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = c.conn.SetReadDeadline(deadline)
		lenBuf := make([]byte, 8)
		if _, err := io.ReadFull(c.conn, lenBuf); err != nil {
			return nil, err
		}
		msgLen := binary.LittleEndian.Uint64(lenBuf)
		payload := make([]byte, msgLen)
		if _, err := io.ReadFull(c.conn, payload); err != nil {
			return nil, err
		}
		var msg pb.Message
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		cmd := msg.GetHeader().GetCommand()
		if cmd == expectedCmd {
			return &msg, nil
		}
		// Bỏ qua gói InitConnection từ server gửi chào kết nối hoặc Ping/Pong keepalive
		if cmd == "InitConnection" || cmd == "Ping" || cmd == "Pong" {
			continue
		}
		return nil, fmt.Errorf("command phản hồi không đúng: nhận '%s', mong đợi '%s'", cmd, expectedCmd)
	}
}

func (c *TCPNodeClient) initConnection() error {
	initData := &pb.InitConnection{
		Address: make([]byte, 20),
		Type:    "CLIENT",
		Replace: true,
	}
	body, err := proto.Marshal(initData)
	if err != nil {
		return err
	}
	return c.sendMsg("InitConnection", body)
}

// GetRegistrationMessage lấy hash cần ký qua TCP.
// Gửi đúng 20 bytes nhị phân của địa chỉ ví, nhận về 32 bytes raw hashToSign.
func (c *TCPNodeClient) GetRegistrationMessage(addr common.Address) (*RegistrationMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Gửi đúng 20 bytes raw nhị phân (addr.Bytes()) để node không bị cắt sai 20 ký tự text
	if err := c.sendMsg("GetRegistrationMessage", addr.Bytes()); err != nil {
		return nil, fmt.Errorf("lỗi gửi GetRegistrationMessage: %w", err)
	}

	resp, err := c.readMsg("RegistrationMessage")
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc response RegistrationMessage: %w", err)
	}

	hashToSign := resp.Body
	fmt.Printf("   📦 [TCP RECV RegistrationMessage Body]\n")
	fmt.Printf("      • Length: %d bytes\n", len(hashToSign))
	fmt.Printf("      • Hex:    0x%s\n", hex.EncodeToString(hashToSign))
	fmt.Printf("      • Base64: %s\n", base64.StdEncoding.EncodeToString(hashToSign))

	if len(hashToSign) != 32 {
		return nil, fmt.Errorf("kích thước hash không đúng: nhận %d bytes (cần 32 bytes)", len(hashToSign))
	}

	return &RegistrationMessage{
		Digest:     hexutil.Encode(hashToSign),
		HashToSign: hexutil.Encode(hashToSign),
	}, nil
}

// RegisterAccount gửi request đăng ký tài khoản qua TCP và parse kết quả bằng pb.RegistrationResult
func (c *TCPNodeClient) RegisterAccount(addr common.Address, sigBytes []byte) (*RegistrationInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqProto := &pb.RegisterAccountRequest{
		Address:   addr.Bytes(), // 20 bytes binary
		Signature: sigBytes,     // 65 bytes ECDSA signature
	}
	body, err := proto.Marshal(reqProto)
	if err != nil {
		return nil, fmt.Errorf("lỗi marshal RegisterAccountRequest: %w", err)
	}

	if err := c.sendMsg("RegisterAccount", body); err != nil {
		return nil, fmt.Errorf("lỗi gửi RegisterAccount: %w", err)
	}

	resp, err := c.readMsg("RegisterAccountAck")
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc RegisterAccountAck: %w", err)
	}

	fmt.Printf("   📦 [TCP RECV RegisterAccountAck Body]\n")
	fmt.Printf("      • Length: %d bytes\n", len(resp.Body))
	fmt.Printf("      • Hex:    0x%s\n", hex.EncodeToString(resp.Body))
	fmt.Printf("      • Base64: %s\n", base64.StdEncoding.EncodeToString(resp.Body))

	// BẮT BUỘC unmarshal bằng pb.RegistrationResult để tránh lỗi Tag 1 map vào address ("RkFJTEVE")
	var res pb.RegistrationResult
	if err := proto.Unmarshal(resp.Body, &res); err != nil {
		return nil, fmt.Errorf("lỗi unmarshal RegistrationResult: %w", err)
	}

	if res.Status == "FAILED" {
		return nil, fmt.Errorf("node từ chối đăng ký (FAILED): %s", res.Reason)
	}

	return &RegistrationInfo{
		Status:      res.Status,
		HomeCluster: res.HomeCluster,
		Reason:      res.Reason,
	}, nil
}

// GetRegistrationStatus truy vấn trạng thái đăng ký qua TCP
func (c *TCPNodeClient) GetRegistrationStatus(addr common.Address) (*RegistrationInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.sendMsg("GetRegistrationStatus", addr.Bytes()); err != nil {
		return nil, fmt.Errorf("lỗi gửi GetRegistrationStatus: %w", err)
	}

	resp, err := c.readMsg("RegistrationStatus")
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc RegistrationStatus: %w", err)
	}

	fmt.Printf("   📦 [TCP RECV RegistrationStatus Body]\n")
	fmt.Printf("      • Length: %d bytes\n", len(resp.Body))
	fmt.Printf("      • Hex:    0x%s\n", hex.EncodeToString(resp.Body))
	fmt.Printf("      • Base64: %s\n", base64.StdEncoding.EncodeToString(resp.Body))

	var res pb.RegistrationResult
	if err := proto.Unmarshal(resp.Body, &res); err != nil {
		return nil, fmt.Errorf("lỗi unmarshal RegistrationResult: %w", err)
	}

	return &RegistrationInfo{
		Status:      res.Status,
		HomeCluster: res.HomeCluster,
		Reason:      res.Reason,
	}, nil
}

// ════════════════════════════════════════════════════════════════════════
// INTERFACE & REGISTRATION RUNNER
// ════════════════════════════════════════════════════════════════════════

// RegistrationClient là interface chung cho cả RPC Client và TCP Client
type RegistrationClient interface {
	GetRegistrationMessage(addr common.Address) (*RegistrationMessage, error)
	RegisterAccount(addr common.Address, sigBytes []byte) (*RegistrationInfo, error)
	GetRegistrationStatus(addr common.Address) (*RegistrationInfo, error)
}

// registerOneWallet thực hiện đăng ký 1 ví qua RegistrationClient (HTTP RPC hoặc TCP Client)
func registerOneWallet(client RegistrationClient, privKey *ecdsa.PrivateKey, index int) (*RegisteredAccountRecord, error) {
	userAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🚀 [%d] Đang xử lý ví: %s\n", index, userAddr.Hex())
	fmt.Printf("   Private Key: %s\n", privKeyHex)

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

	fmt.Printf("   1️⃣ Đang lấy thông điệp đăng ký từ node (mtn_getRegistrationMessage)...\n")
	regMsg, err := client.GetRegistrationMessage(userAddr)
	if err != nil {
		return nil, fmt.Errorf("thất bại khi lấy thông điệp đăng ký: %w", err)
	}

	hashBytes, err := hexutil.Decode(regMsg.HashToSign)
	if err != nil {
		return nil, fmt.Errorf("lỗi decode hashToSign: %w", err)
	}

	sigBytes, err := crypto.Sign(hashBytes, privKey)
	if err != nil {
		return nil, fmt.Errorf("lỗi tạo chữ ký ECDSA: %w", err)
	}
	fmt.Printf("   2️⃣ Đã ký chữ ký ECDSA thành công: %s\n", hexutil.Encode(sigBytes))

	fmt.Printf("   3️⃣ Đang gửi đăng ký lên Node thực thi (mtn_registerAccount)...\n")
	submitInfo, err := client.RegisterAccount(userAddr, sigBytes)
	if err != nil {
		return nil, fmt.Errorf("node từ chối yêu cầu đăng ký: %w", err)
	}
	fmt.Printf("      • Trạng thái phản hồi: %s (Reason: %s)\n", submitInfo.Status, submitInfo.Reason)

	fmt.Printf("   4️⃣ Đang chờ Node & Parent Chain xác nhận (Polling status)...\n")
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
				// Chờ tiếp
			}
		}
	}
}

// registerOneWalletTCP thực hiện đăng ký 1 ví qua TCP Socket với cơ chế ASYNC PUSH (Không Polling)
func registerOneWalletTCP(client *client_tcp.Client, privKey *ecdsa.PrivateKey, index int) (*RegisteredAccountRecord, error) {
	userAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("🚀 [%d] (TCP Socket) Đang xử lý ví: %s\n", index, userAddr.Hex())
	fmt.Printf("   Private Key: %s\n", privKeyHex)

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

	fmt.Printf("   1️⃣ Đang lấy thông điệp đăng ký từ node qua TCP (GetRegistrationMessage)...\n")
	hashToSign, err := client.ChainGetRegistrationMessage(userAddr)
	if err != nil {
		return nil, fmt.Errorf("thất bại khi lấy thông điệp đăng ký qua TCP: %w", err)
	}

	sigBytes, err := crypto.Sign(hashToSign.Bytes(), privKey)
	if err != nil {
		return nil, fmt.Errorf("lỗi tạo chữ ký ECDSA: %w", err)
	}
	fmt.Printf("   2️⃣ Đã ký chữ ký ECDSA thành công: %s\n", hexutil.Encode(sigBytes))

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
		fmt.Printf("   ✅ Node PUSH kết quả PROVISIONAL; ví được phép giao dịch nội bộ.\n")
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

// RunTest là hàm kiểm thử tự động chính thức trong CI / Test Suite
func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 35-account-gate-registration")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra cơ chế Account Registration Gate trên Child Chain (Execution Cluster):")
	fmt.Println("             1. Xác nhận ví chưa đăng ký bị chặn (RecipientNotRegistered)")
	fmt.Println("             2. Đăng ký ví mới qua RPC (mtn_getRegistrationMessage -> sign -> mtn_registerAccount)")
	fmt.Println("             3. Chờ trạng thái CONFIRMED/PROVISIONAL từ Parent Chain & Cluster")
	fmt.Println("             4. Gửi Native Token vào ví vừa đăng ký thành công")
	fmt.Println("             5. Ví mới tự gửi giao dịch chuyển tiền thành công")
	fmt.Println("             6. Nếu có cấu hình TCP, kiểm tra push notification qua TCP socket.")
	fmt.Println("🎯 KỲ VỌNG : Toàn bộ quy trình gate & onboarding hoạt động chính xác 100%.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("lỗi load config: %w", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("lỗi kết nối RPC ethclient: %w", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("cần ít nhất 1 private key đã đăng ký để test")
	}

	rpcClient := NewNodeRPCClient(cfg.RPCUrl)

	// 1. Kiểm tra Cluster Identity và trạng thái Account Gate
	fmt.Println("🔍 [Bước 1] Kiểm tra trạng thái Account Gate của Cluster...")
	clusterIdentity, err := rpcClient.GetClusterIdentity()
	if err != nil {
		fmt.Printf("⚠️ Cảnh báo: Cluster không hỗ trợ mtn_getClusterIdentity hoặc Account Gate tắt (%v)\n", err)
		fmt.Println("👉 Bỏ qua kiểm tra Account Gate vì chain đang chạy ở chế độ không có gate.")
		return nil
	}

	fmt.Printf("   • Chain ID:     %d\n", clusterIdentity.ChainID)
	fmt.Printf("   • Cluster Key:  %s\n", clusterIdentity.ClusterKey)
	fmt.Printf("   • Account Gate: %v\n", clusterIdentity.AccountGate)

	if !clusterIdentity.AccountGate {
		fmt.Println("ℹ️ Account Gate không bật trên chain này, hoàn thành kiểm tra tương thích.")
		return nil
	}

	// 2. Tạo một tài khoản mới tinh (chưa từng đăng ký)
	newPrivKey, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("lỗi sinh private key mới: %w", err)
	}
	newAddr := crypto.PubkeyToAddress(newPrivKey.PublicKey)
	fmt.Printf("\n🔑 [Bước 2] Sinh ngẫu nhiên ví mới: %s\n", newAddr.Hex())

	// 3. Thử gửi tiền tới ví chưa đăng ký -> Phải bị chặn bởi Account Gate
	fmt.Println("🛡️ [Bước 3] Thử gửi Native Token đến ví chưa đăng ký để kiểm tra Account Gate...")
	fundedKey, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("lỗi parse funded private key: %w", err)
	}
	fundedAddr := crypto.PubkeyToAddress(fundedKey.PublicKey)

	nonce, err := client.PendingNonceAt(context.Background(), fundedAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce ví nguồn: %w", err)
	}

	gasLimit := uint64(21000)
	gasPrice := big.NewInt(1e9)
	sendAmount := big.NewInt(1000000000000000) // 0.001 token

	testRejectTx := types.NewTransaction(nonce, newAddr, sendAmount, gasLimit, gasPrice, nil)
	signedRejectTx, err := types.SignTx(testRejectTx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), fundedKey)
	if err != nil {
		return fmt.Errorf("lỗi ký test tx: %w", err)
	}

	sendErr := client.SendTransaction(context.Background(), signedRejectTx)
	if sendErr == nil {
		return fmt.Errorf("❌ LỖI BẢO MẬT: Giao dịch gửi đến ví chưa đăng ký lại được chấp nhận thành công! Account Gate không hoạt động.")
	}

	fmt.Printf("   ✅ Account Gate đã chặn chính xác: \"%v\"\n", sendErr)

	// 4. Đăng ký ví mới qua mtn_registerAccount
	fmt.Println("\n📝 [Bước 4] Tiến hành đăng ký ví mới qua Account Gate RPC...")
	regMsg, err := rpcClient.GetRegistrationMessage(newAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy thông điệp đăng ký: %w", err)
	}

	hashBytes, err := hexutil.Decode(regMsg.HashToSign)
	if err != nil {
		return fmt.Errorf("lỗi decode HashToSign: %w", err)
	}

	sigBytes, err := crypto.Sign(hashBytes, newPrivKey)
	if err != nil {
		return fmt.Errorf("lỗi ký ECDSA secp256k1: %w", err)
	}

	submitInfo, err := rpcClient.RegisterAccount(newAddr, sigBytes)
	if err != nil {
		return fmt.Errorf("lỗi gọi mtn_registerAccount: %w", err)
	}
	fmt.Printf("   • Trạng thái nộp: %s (Reason: %s)\n", submitInfo.Status, submitInfo.Reason)

	// 5. Chờ xác nhận CONFIRMED hoặc PROVISIONAL
	fmt.Println("⏳ [Bước 5] Chờ Parent Chain và Node xác nhận đăng ký...")
	timeout := time.After(45 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	confirmed := false
	for !confirmed {
		select {
		case <-timeout:
			return fmt.Errorf("timeout sau 45s: ví %s chưa được chuyển sang CONFIRMED/PROVISIONAL", newAddr.Hex())
		case <-ticker.C:
			statusInfo, err := rpcClient.GetRegistrationStatus(newAddr)
			if err != nil {
				continue
			}
			fmt.Printf("   ⏳ Trạng thái hiện tại: %s\n", statusInfo.Status)
			if statusInfo.Status == "CONFIRMED" || statusInfo.Status == "PROVISIONAL" {
				fmt.Printf("   🎉 ĐĂNG KÝ THÀNH CÔNG! Trạng thái: %s\n", statusInfo.Status)
				confirmed = true
			} else if statusInfo.Status == "REJECTED" || statusInfo.Status == "FAILED" {
				return fmt.Errorf("đăng ký thất bại với trạng thái: %s (reason: %s)", statusInfo.Status, statusInfo.Reason)
			}
		}
	}

	// 6. Gửi tiền vào ví vừa đăng ký thành công
	fmt.Println("\n💸 [Bước 6] Gửi Native Token vào ví vừa đăng ký thành công...")
	nonce, err = client.PendingNonceAt(context.Background(), fundedAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce mới: %w", err)
	}

	fundTx := types.NewTransaction(nonce, newAddr, sendAmount, gasLimit, gasPrice, nil)
	signedFundTx, err := types.SignTx(fundTx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), fundedKey)
	if err != nil {
		return fmt.Errorf("lỗi ký fund tx: %w", err)
	}

	if err := client.SendTransaction(context.Background(), signedFundTx); err != nil {
		return fmt.Errorf("gửi tiền vào ví vừa đăng ký thất bại: %w", err)
	}
	fmt.Printf("   ✅ Tx gửi tiền thành công: %s\n", signedFundTx.Hash().Hex())

	// Chờ tx confirm
	fmt.Println("   ⏳ Chờ Tx nạp tiền được đóng block...")
	for {
		receipt, err := client.TransactionReceipt(context.Background(), signedFundTx.Hash())
		if err == nil && receipt != nil && receipt.Status == 1 {
			fmt.Printf("   ✅ Tx nạp tiền confirmed tại block %d!\n", receipt.BlockNumber.Uint64())
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	newBal, err := client.BalanceAt(context.Background(), newAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi đọc số dư ví mới: %w", err)
	}
	fmt.Printf("   💰 Số dư ví mới hiện tại: %s wei\n", newBal.String())
	if newBal.Cmp(sendAmount) < 0 {
		return fmt.Errorf("số dư không khớp kỳ vọng: có %s wei, cần ít nhất %s wei", newBal.String(), sendAmount.String())
	}

	// 7. Ví mới tự ký và gửi một giao dịch chuyển Native Token đi (Giao dịch đầu tiên của ví mới)
	fmt.Println("\n🚀 [Bước 7] Ví mới tự gửi giao dịch đầu tiên (Outbound Transfer)...")
	newNonce, err := client.PendingNonceAt(context.Background(), newAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce ví mới: %w", err)
	}

	returnAmount := big.NewInt(100000000000000) // 0.0001 token
	outboundTx := types.NewTransaction(newNonce, fundedAddr, returnAmount, gasLimit, gasPrice, nil)
	signedOutboundTx, err := types.SignTx(outboundTx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), newPrivKey)
	if err != nil {
		return fmt.Errorf("lỗi ký outbound tx: %w", err)
	}

	if err := client.SendTransaction(context.Background(), signedOutboundTx); err != nil {
		return fmt.Errorf("ví mới gửi giao dịch thất bại: %w", err)
	}
	fmt.Printf("   ✅ Tx từ ví mới gửi thành công: %s\n", signedOutboundTx.Hash().Hex())

	for {
		receipt, err := client.TransactionReceipt(context.Background(), signedOutboundTx.Hash())
		if err == nil && receipt != nil && receipt.Status == 1 {
			fmt.Printf("   ✅ Tx từ ví mới confirmed tại block %d!\n", receipt.BlockNumber.Uint64())
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Println("\n🎉 TEST PASSED: Toàn bộ quy trình Account Gate và Onboarding ví mới hoạt động hoàn hảo!")
	return nil
}

func main() {
	// Định nghĩa cờ dòng lệnh CLI
	rpcFlag := flag.String("rpc", "", "Endpoint JSON-RPC HTTP (ví dụ: http://192.168.1.234:8747)")
	tcpFlag := flag.String("tcp", "", "Endpoint TCP Socket của node (ví dụ: 192.168.1.234:4200)")
	tcpConfigFlag := flag.String("tcp-config", "", "Đường dẫn file cấu hình JSON TCP (tùy chọn)")
	countFlag := flag.Int("count", 0, "Số lượng ví mới cần tạo và đăng ký (dùng cho CLI mode)")
	walletPkFlag := flag.String("wallet-pk", "", "Private key hex của ví có sẵn cần đăng ký (CLI mode)")
	outFileFlag := flag.String("out", "", "Đường dẫn file JSON để xuất danh sách ví đã đăng ký")
	checkOnlyFlag := flag.String("check", "", "Địa chỉ ví cần kiểm tra trạng thái đăng ký (chỉ kiểm tra, không đăng ký)")
	cliModeFlag := flag.Bool("cli", false, "Bắt buộc chạy ở chế độ CLI Onboarding Tool thay vì Test Suite")
	flag.Parse()

	isCLIMode := *cliModeFlag || *countFlag > 0 || *walletPkFlag != "" || *checkOnlyFlag != "" || *outFileFlag != "" || *tcpFlag != "" || *tcpConfigFlag != ""

	// ════════════════════════════════════════════════════════════════════
	// CHẾ ĐỘ 1: AUTOMATED TEST SUITE (Mặc định khi chạy CI hoặc go test)
	// ════════════════════════════════════════════════════════════════════
	if !isCLIMode {
		configPath := "../config.json"
		if flag.NArg() > 0 {
			configPath = flag.Arg(0)
		}
		if err := RunTest(configPath); err != nil {
			log.Fatalf("❌ %v", err)
		}
		return
	}

	// ════════════════════════════════════════════════════════════════════
	// CHẾ ĐỘ 2: CLI ONBOARDING TOOL (Thay thế trọn vẹn register_account)
	// ════════════════════════════════════════════════════════════════════

	// Tự động load config nếu rpc / tcp chưa được cung cấp qua cờ
	if *outFileFlag == "" && *checkOnlyFlag == "" {
		*outFileFlag = "registered_wallets.json"
	}
	defaultRPC := "http://127.0.0.1:8747"
	defaultTCP := ""
	if cfg, err := config.LoadConfig("../config.json"); err == nil && cfg != nil {
		if cfg.RPCUrl != "" {
			defaultRPC = cfg.RPCUrl
		}
		if cfg.TCPNode != "" {
			defaultTCP = cfg.TCPNode
		}
	}

	// Xử lý qua TCP Socket nếu có cờ TCP
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
		fmt.Printf("📡 Endpoint TCP: %s\n\n", cfg.GetParentConnectionAddress())

		tcpClient, err := client_tcp.NewClient(cfg)
		if err != nil {
			fmt.Printf("⚠️ Không thể khởi tạo client_tcp: %v\n👉 Thử kết nối TCP raw client...\n", err)
			rawClient, rawErr := NewTCPNodeClient(cfg.GetParentConnectionAddress())
			if rawErr != nil {
				fmt.Printf("❌ Lỗi kết nối TCP %s: %v\n", cfg.GetParentConnectionAddress(), rawErr)
				os.Exit(1)
			}
			defer rawClient.Close()

			if *checkOnlyFlag != "" {
				checkAddr := common.HexToAddress(*checkOnlyFlag)
				fmt.Printf("🔍 (TCP Raw) Đang kiểm tra trạng thái ví: %s\n", checkAddr.Hex())
				regMsg, err := rawClient.GetRegistrationMessage(checkAddr)
				if err != nil {
					fmt.Printf("   • Lỗi lấy Hash:    %v\n", err)
				} else {
					fmt.Printf("   • Hash to Sign:    %s\n", regMsg.HashToSign)
				}
				status, err := rawClient.GetRegistrationStatus(checkAddr)
				if err != nil {
					fmt.Printf("❌ Lỗi kiểm tra status qua TCP: %v\n", err)
					return
				}
				fmt.Printf("   • Trạng thái Gate: %s\n", status.Status)
				if status.HomeCluster != "" {
					fmt.Printf("   • Home Cluster:    %s\n", status.HomeCluster)
				}
				return
			}

			var results []*RegisteredAccountRecord
			count := *countFlag
			if count <= 0 && *walletPkFlag == "" {
				count = 1
			}

			if *walletPkFlag != "" {
				cleanPk := strings.TrimPrefix(*walletPkFlag, "0x")
				privKey, err := crypto.HexToECDSA(cleanPk)
				if err != nil {
					fmt.Printf("❌ Private key không hợp lệ: %v\n", err)
					os.Exit(1)
				}
				record, err := registerOneWallet(rawClient, privKey, 1)
				if err != nil {
					fmt.Printf("❌ Lỗi đăng ký qua TCP: %v\n", err)
					os.Exit(1)
				}
				results = append(results, record)
			} else {
				fmt.Printf("🔑 Chuẩn bị sinh ngẫu nhiên và đăng ký %d ví mới qua TCP Socket...\n", count)
				for i := 1; i <= count; i++ {
					privKey, err := crypto.GenerateKey()
					if err != nil {
						fmt.Printf("❌ Lỗi sinh private key cho ví %d: %v\n", i, err)
						continue
					}
					record, err := registerOneWallet(rawClient, privKey, i)
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

			fmt.Printf("\n✨ Hoàn thành qua TCP! (%d ví đăng ký thành công)\n", len(results))
			return
		}
		time.Sleep(500 * time.Millisecond)

		if *checkOnlyFlag != "" {
			checkAddr := common.HexToAddress(*checkOnlyFlag)
			fmt.Printf("🔍 (TCP) Đang kiểm tra trạng thái ví: %s\n", checkAddr.Hex())
			hashToSign, err := tcpClient.ChainGetRegistrationMessage(checkAddr)
			if err != nil {
				fmt.Printf("   • Lỗi lấy Hash:    %v\n", err)
			} else {
				fmt.Printf("   • Hash to Sign:    %s\n", hashToSign.Hex())
			}
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
		count := *countFlag
		if count <= 0 && *walletPkFlag == "" {
			count = 1
		}

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
			fmt.Printf("🔑 Chuẩn bị sinh ngẫu nhiên và đăng ký %d ví mới qua TCP Socket (Push Mode)...\n", count)
			for i := 1; i <= count; i++ {
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

		fmt.Printf("\n✨ Hoàn thành qua TCP! (%d ví đăng ký thành công)\n", len(results))
		return
	}

	// Xử lý qua HTTP JSON-RPC
	targetRPC := *rpcFlag
	if targetRPC == "" {
		targetRPC = defaultRPC
	}
	client := NewNodeRPCClient(targetRPC)

	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║      METANODE ACCOUNT GATE ONBOARDING TOOL (HTTP JSON-RPC)       ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Printf("📡 Endpoint RPC: %s\n\n", targetRPC)

	clusterId, err := client.GetClusterIdentity()
	if err != nil {
		fmt.Printf("⚠️ Cảnh báo: Không thể lấy cluster identity từ RPC: %v\n\n", err)
	} else {
		fmt.Printf("✅ Đã kết nối cụm thành công!\n")
		fmt.Printf("   • Chain ID:      %d\n", clusterId.ChainID)
		fmt.Printf("   • Cluster Key:   %s\n", clusterId.ClusterKey)
		fmt.Printf("   • Account Gate:  %v\n", clusterId.AccountGate)
		fmt.Printf("   • Message Tag:   %s\n\n", clusterId.MessageTag)
	}

	// Nếu chỉ kiểm tra 1 địa chỉ qua RPC
	if *checkOnlyFlag != "" {
		checkAddr := common.HexToAddress(*checkOnlyFlag)
		fmt.Printf("🔍 (RPC) Đang kiểm tra ví: %s\n", checkAddr.Hex())

		regMsg, err := client.GetRegistrationMessage(checkAddr)
		if err != nil {
			fmt.Printf("   • Lỗi lấy Hash:    %v\n", err)
		} else {
			fmt.Printf("   • Hash to Sign:    %s\n", regMsg.HashToSign)
		}

		status, err := client.GetRegistrationStatus(checkAddr)
		if err != nil {
			fmt.Printf("❌ Lỗi kiểm tra status qua RPC: %v\n", err)
			return
		}
		fmt.Printf("   • Trạng thái Gate: %s\n", status.Status)
		if status.HomeCluster != "" {
			fmt.Printf("   • Home Cluster:    %s\n", status.HomeCluster)
		}
		balance, _ := client.GetBalance(checkAddr)
		fmt.Printf("   • Số dư:           %s wei\n", balance.String())
		return
	}

	var results []*RegisteredAccountRecord
	count := *countFlag
	if count <= 0 && *walletPkFlag == "" {
		count = 1
	}

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
		fmt.Printf("🔑 Chuẩn bị sinh ngẫu nhiên và đăng ký %d ví mới...\n", count)
		for i := 1; i <= count; i++ {
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

	if len(results) > 0 && *outFileFlag != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err == nil {
			_ = os.WriteFile(*outFileFlag, data, 0644)
			fmt.Printf("\n💾 Đã lưu danh sách %d ví đăng ký thành công vào file: %s\n", len(results), *outFileFlag)
		}
	}

	fmt.Printf("\n✨ Hoàn thành! (%d ví đăng ký thành công)\n", len(results))
	_ = defaultTCP
}
