package main

import (
	"fmt"
	"log"
	"os"
	"time"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	"tool-test/pkg/logger"

	"github.com/ethereum/go-ethereum/common"
)

func main() {
	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	fmt.Println("==================================================")
	fmt.Println("🚀 START TEST RACE CONDITION (NOMT READ ERROR)")
	fmt.Println("==================================================")

	// Load TCP Config
	cfgRaw, err := tcp_config.LoadConfig("../caller-tcp/config-local.json")
	if err != nil {
		log.Fatalf("❌ Load config error: %v", err)
	}
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	// Connect to TCP
	tcpClient, err := client_tcp.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Connect TCP %s failed: %v", cfg.ConnectionAddress(), err)
	}
	time.Sleep(1 * time.Second)

	fmt.Println("✅ TCP Connected!")

	// Test Address (replace with the one causing the error)
	testAddress := common.HexToAddress("0x824fef8A3cE4b93C546209CC254D97E5Fee804e0")
	fmt.Printf("🔍 Spamming GetAccountState for %s...\n", testAddress.Hex())

	successCount := 0
	errorCount := 0
	timeoutCount := 0

	for i := 0; i < 10000; i++ { // 10000 requests
		// Send GetAccountState
		_, err := tcpClient.GetAccountState(testAddress, 10*time.Second)
		if err != nil {
			log.Fatalf("❌ GetAccountState error: %v", err)
		}

		successCount++
		if successCount%100 == 0 {
			fmt.Printf("✅ Received 100 AccountState responses (Total: %d)\n", successCount)
		}

		time.Sleep(10 * time.Millisecond) // Spam frequency
	}

	fmt.Println("==================================================")
	fmt.Printf("🏁 TEST COMPLETED. Success: %d, Timeout: %d, Send Errors: %d\n", successCount, timeoutCount, errorCount)
	fmt.Println("==================================================")
}
