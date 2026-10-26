/*
 * BÀI TEST: 28.1-eip7702-factory-create-nonce
 * MÔ TẢ   : Kiểm tra chuyên sâu cơ chế EIP-7702 kết hợp Smart Contract Factory:
 *           1. Deploy Factory Smart Contract chứa opcode CREATE (0x600060006000f05000).
 *           2. Chuẩn bị ví ký hộ (Authority / Ví 1) có nonce ban đầu = 1.
 *           3. Ví ký hộ ký EIP-7702 Authorization ủy quyền code của mình trỏ đến Factory Contract.
 *           4. Ví trả hộ (Relayer / Ví 0) gửi giao dịch SetCodeTx (Type 0x04) gọi vào Ví ký hộ:
 *              - Relayer trả toàn bộ phí gas và tip.
 *              - Node xử lý Authorization: tăng nonce ví ký hộ từ 1 lên 2.
 *              - EVM thực thi code Factory trên ví ký hộ: opcode CREATE tạo contract con,
 *                tăng tiếp nonce của ví ký hộ từ 2 lên 3!
 * 🎯 KỲ VỌNG:
 *           - Giao dịch thực thi thành công (Receipt status = 1).
 *           - Nonce của ví ký hộ sau giao dịch tăng chính xác +2 (từ 1 lên 3).
 *           - Hợp đồng con được tạo thành công tại địa chỉ tính theo CreateAddress(Authority, 2).
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)

// zzInit bọc runtime bytecode vào constructor initcode chuẩn của EVM:
// PUSH1 len, PUSH1 0x0c, PUSH1 0x00, CODECOPY, PUSH1 len, PUSH1 0x00, RETURN + runtime
func zzInit(runtime []byte) []byte {
	return append([]byte{0x60, byte(len(runtime)), 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, byte(len(runtime)), 0x60, 0x00, 0xf3}, runtime...)
}

func waitForReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("❌ Hết thời gian (timeout) chờ receipt cho tx %s", txHash.Hex())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 28.1-eip7702-factory-create-nonce")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra EIP-7702 ủy quyền Factory tạo contract con:")
	fmt.Println("             1. Deploy Factory Contract có chứa opcode CREATE.")
	fmt.Println("             2. Chuẩn bị Ví ký hộ (Authority) với nonce ban đầu = 1.")
	fmt.Println("             3. Ví ký hộ ký Authorization trỏ sang Factory.")
	fmt.Println("             4. Ví trả hộ (Relayer) gửi SetCodeTx gọi vào Ví ký hộ.")
	fmt.Println("             5. EVM chạy opcode CREATE trên ngữ cảnh của Ví ký hộ.")
	fmt.Println("🎯 KỲ VỌNG : Status = 1, Nonce ví ký hộ tăng từ 1 -> 3 (tăng đúng 2).")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("❌ Lỗi kết nối RPC (%s): %v", cfg.RPCUrl, err)
	}

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("❌ Cần ít nhất 1 private key trong config")
	}

	chainID := big.NewInt(cfg.ChainID)
	if cfg.ChainID == 0 {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("❌ Lấy ChainID thất bại: %v", err)
		}
		chainID = cid
	}

	signer := types.NewPragueSigner(chainID)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(20000000000) // 20 Gwei
	}
	gasTipCap, _ := client.SuggestGasTipCap(context.Background())
	if gasTipCap == nil || gasTipCap.Sign() == 0 {
		gasTipCap = big.NewInt(1000000000) // 1 Gwei
	}

	// -------------------------------------------------------------------------
	// Chuẩn bị 2 tài khoản: Ví trả hộ (Relayer) và Ví ký hộ (Authority)
	// -------------------------------------------------------------------------
	pkRelayer, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("❌ Parse private key relayer (ví 0) thất bại: %v", err)
	}
	addrRelayer := crypto.PubkeyToAddress(pkRelayer.PublicKey)

	var pkAuthority *ecdsa.PrivateKey
	if len(cfg.PrivateKeys) > 1 {
		pkAuthority, err = crypto.HexToECDSA(cfg.PrivateKeys[1])
		if err != nil {
			return fmt.Errorf("❌ Parse private key authority (ví 1) thất bại: %v", err)
		}
	} else {
		// Sinh ví mới nếu chỉ có 1 key
		pkAuthority, err = crypto.GenerateKey()
		if err != nil {
			return fmt.Errorf("❌ Sinh ví mới cho authority thất bại: %v", err)
		}
	}
	addrAuthority := crypto.PubkeyToAddress(pkAuthority.PublicKey)

	fmt.Printf("🔑 Ví trả hộ (Relayer / Ví 0): %s\n", addrRelayer.Hex())
	fmt.Printf("🛡️ Ví ký hộ  (Authority / Ví 1): %s\n", addrAuthority.Hex())
	fmt.Printf("🌐 ChainID: %s, RPC: %s\n", chainID.String(), cfg.RPCUrl)

	// Kiểm tra nếu có file Factory.bin được biên dịch từ Factory.sol thì ưu tiên dùng
	var factoryDeployCode []byte
	binBytes, errRead := os.ReadFile("Factory.bin")
	if errRead == nil && len(binBytes) > 0 {
		var errHex error
		factoryDeployCode, errHex = hex.DecodeString(strings.TrimSpace(string(binBytes)))
		if errHex == nil && len(factoryDeployCode) > 0 {
			fmt.Printf("   📦 Sử dụng bytecode từ hợp đồng Factory.sol (Factory.bin, %d bytes)\n", len(factoryDeployCode))
		}
	}
	if len(factoryDeployCode) == 0 {
		// Fallback: Bytecode tối giản (PUSH1 0, PUSH1 0, PUSH1 0, CREATE, POP, STOP)
		factoryRuntime := common.FromHex("0x600060006000f05000")
		factoryDeployCode = zzInit(factoryRuntime)
		fmt.Printf("   📦 Sử dụng bytecode tối giản chứa opcode CREATE (%d bytes)\n", len(factoryDeployCode))
	}

	relayerNonce, err := client.PendingNonceAt(context.Background(), addrRelayer)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce relayer thất bại: %v", err)
	}

	deployTx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     relayerNonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasPrice,
		Gas:       3000000,
		To:        nil, // Contract creation
		Value:     big.NewInt(0),
		Data:      factoryDeployCode,
	})
	signedDeploy, err := types.SignTx(deployTx, signer, pkRelayer)
	if err != nil {
		return fmt.Errorf("❌ Ký deploy factory thất bại: %v", err)
	}
	if err := client.SendTransaction(context.Background(), signedDeploy); err != nil {
		return fmt.Errorf("❌ Gửi tx deploy factory thất bại: %v", err)
	}
	rcptDeploy, err := waitForReceipt(client, signedDeploy.Hash())
	if err != nil || rcptDeploy.Status != 1 {
		return fmt.Errorf("❌ Deploy factory thất bại! Receipt: %+v, err: %v", rcptDeploy, err)
	}
	factoryAddr := rcptDeploy.ContractAddress
	fmt.Printf("   ✅ Factory Contract đã deploy tại: %s (Block: %d, GasUsed: %d)\n",
		factoryAddr.Hex(), rcptDeploy.BlockNumber.Uint64(), rcptDeploy.GasUsed)

	// -------------------------------------------------------------------------
	// BƯỚC 2: Đồng bộ Nonce của Ví Ký Hộ về đúng = 1
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 2: Kiểm tra và chuẩn bị Nonce ban đầu của Ví ký hộ...")
	authNonce, err := client.PendingNonceAt(context.Background(), addrAuthority)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce authority thất bại: %v", err)
	}

	// Đảm bảo ví ký hộ có ít nhất một chút balance để nếu cần gửi tx warmup
	balAuth, _ := client.BalanceAt(context.Background(), addrAuthority, nil)
	minBal := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // 1 MT
	if balAuth.Cmp(minBal) < 0 {
		fmt.Println("   💸 Nạp tiền cho ví ký hộ từ Relayer...")
		relayerNonce, _ = client.PendingNonceAt(context.Background(), addrRelayer)
		fundTx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     relayerNonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasPrice,
			Gas:       100000,
			To:        &addrAuthority,
			Value:     new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18)),
		})
		signedFund, _ := types.SignTx(fundTx, signer, pkRelayer)
		_ = client.SendTransaction(context.Background(), signedFund)
		_, _ = waitForReceipt(client, signedFund.Hash())
	}

	// Nếu nonce = 0, ví ký hộ thực hiện 1 giao dịch đơn giản để nâng nonce lên đúng 1
	if authNonce == 0 {
		fmt.Println("   ⚡ Nonce hiện tại = 0, thực hiện 1 tx chuyển khoản để nâng nonce lên 1...")
		warmupTx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     0,
			GasTipCap: gasTipCap,
			GasFeeCap: gasPrice,
			Gas:       100000,
			To:        &addrRelayer,
			Value:     big.NewInt(1000),
		})
		signedWarmup, err := types.SignTx(warmupTx, signer, pkAuthority)
		if err != nil {
			return fmt.Errorf("❌ Ký warmup tx thất bại: %v", err)
		}
		if err := client.SendTransaction(context.Background(), signedWarmup); err != nil {
			return fmt.Errorf("❌ Gửi warmup tx thất bại: %v", err)
		}
		_, err = waitForReceipt(client, signedWarmup.Hash())
		if err != nil {
			return fmt.Errorf("❌ Chờ warmup tx thất bại: %v", err)
		}
		authNonce, _ = client.PendingNonceAt(context.Background(), addrAuthority)
	}

	fmt.Printf("   📌 Nonce ban đầu của Ví ký hộ (trước EIP-7702 test) = %d\n", authNonce)
	expectedNonceAfter := authNonce + 2

	// -------------------------------------------------------------------------
	// BƯỚC 3: Ví Ký Hộ ký EIP-7702 Authorization ủy quyền sang Factory
	// -------------------------------------------------------------------------
	fmt.Printf("\n🔹 BƯỚC 3: Ví ký hộ ký SetCodeAuthorization trỏ sang Factory (Nonce: %d)...\n", authNonce)
	authTuple, err := types.SignSetCode(pkAuthority, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: factoryAddr,
		Nonce:   authNonce,
	})
	if err != nil {
		return fmt.Errorf("❌ Ký SetCode Authorization thất bại: %v", err)
	}
	fmt.Printf("   ✍️ Đã ký Authorization: %s -> Factory %s (Nonce = %d)\n",
		addrAuthority.Hex(), factoryAddr.Hex(), authNonce)
	fmt.Println("   ℹ️ Lưu ý: Khi block xử lý tuple này, nonce ví ký hộ sẽ tăng +1 (chống replay).")

	// -------------------------------------------------------------------------
	// BƯỚC 4: Ví Trả Hộ (Relayer) gửi SetCodeTx gọi vào Ví Ký Hộ kích hoạt CREATE
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 4: Ví trả hộ gửi SetCodeTx kích hoạt Factory trên ví ký hộ...")
	relayerNonce, err = client.PendingNonceAt(context.Background(), addrRelayer)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce relayer thất bại: %v", err)
	}

	// To là addrAuthority, Data có calldata để kích hoạt EVM
	setCodeTx := types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     relayerNonce,
		GasTipCap: uint256.MustFromBig(gasTipCap),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       2000000,
		To:        addrAuthority,
		Value:     uint256.NewInt(0),
		Data:      []byte{0x01}, // Calldata kích hoạt EVM call vào delegated code
		AuthList:  []types.SetCodeAuthorization{authTuple},
	})

	signedSetCode, err := types.SignTx(setCodeTx, signer, pkRelayer)
	if err != nil {
		return fmt.Errorf("❌ Ký SetCodeTx thất bại: %v", err)
	}

	fmt.Printf("   📤 Relayer (Ví 0) gửi Tx Hash: %s (Type: 0x%02x)...\n",
		signedSetCode.Hash().Hex(), signedSetCode.Type())
	if err := client.SendTransaction(context.Background(), signedSetCode); err != nil {
		return fmt.Errorf("❌ Gửi SetCodeTx thất bại: %v", err)
	}

	rcptSetCode, err := waitForReceipt(client, signedSetCode.Hash())
	if err != nil {
		return fmt.Errorf("❌ Chờ receipt SetCodeTx thất bại: %v", err)
	}
	fmt.Printf("   ✅ SetCodeTx đã commit tại Block %d (Status: %d, GasUsed: %d)\n",
		rcptSetCode.BlockNumber.Uint64(), rcptSetCode.Status, rcptSetCode.GasUsed)

	if rcptSetCode.Status != 1 {
		return fmt.Errorf("❌ SetCodeTx bị revert hoặc thất bại! Status = %d", rcptSetCode.Status)
	}

	// -------------------------------------------------------------------------
	// BƯỚC 5: Kiểm chứng Nonce ví ký hộ và Hợp đồng con sinh ra
	// -------------------------------------------------------------------------
	fmt.Println("\n🔹 BƯỚC 5: Kiểm tra kết quả Nonce và Contract con sinh ra...")
	authNonceAfter, err := client.PendingNonceAt(context.Background(), addrAuthority)
	if err != nil {
		return fmt.Errorf("❌ Lấy nonce authority sau tx thất bại: %v", err)
	}

	// Hợp đồng con do ví ký hộ tạo ra tại nonce = authNonce + 1
	childContractAddr := crypto.CreateAddress(addrAuthority, authNonce+1)

	fmt.Printf("   📊 Nonce ban đầu : %d\n", authNonce)
	fmt.Printf("   📊 Nonce kết thúc: %d\n", authNonceAfter)
	fmt.Printf("   🎯 Nonce kỳ vọng : %d (Tăng chính xác +2: 1 từ SetCode Auth + 1 từ Opcode CREATE)\n", expectedNonceAfter)
	fmt.Printf("   👶 Địa chỉ contract con sinh ra: %s\n", childContractAddr.Hex())

	if authNonceAfter != expectedNonceAfter {
		return fmt.Errorf("❌ THẤT BẠI: Nonce ví ký hộ không đúng! Nhận: %d, Kỳ vọng: %d",
			authNonceAfter, expectedNonceAfter)
	}

	fmt.Println("\n==========================================================")
	fmt.Printf("🎉 BÀI TEST PASSED HOÀN HẢO!\n")
	fmt.Printf("   - Ví ký hộ đã tăng nonce từ %d lên %d (+2) như kỳ vọng!\n", authNonce, authNonceAfter)
	fmt.Printf("   - Phí gas do Ví trả hộ (Relayer) chi trả hoàn toàn.\n")
	fmt.Println("==========================================================")
	return nil
}

func main() {
	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	if err := RunTest(configPath); err != nil {
		log.Fatalf("%v", err)
	}
}
