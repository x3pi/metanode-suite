package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"google.golang.org/protobuf/proto"

	pb "tool-test/pkg/proto"
)

// TCPNodeClient thực hiện toàn bộ giao tiếp đăng ký tài khoản qua TCP (port 4200/9545).
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
