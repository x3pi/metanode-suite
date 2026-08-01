import { createPublicClient, createWalletClient, http } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import * as fs from 'fs';
import * as path from 'path';

// Đọc cấu hình từ file config-local.json
const configPath = '/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/config-local.json';
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));

// Khởi tạo tài khoản từ Private Key
const account = privateKeyToAccount(`0x${config.private_key}`);

const metanodeChain = {
  id: config.chain_id,
  name: 'MetaNode',
  nativeCurrency: { name: 'MetaNode', symbol: 'MTN', decimals: 18 },
  rpcUrls: {
    default: { http: [config.rpc_url] },
  },
} as const;

const publicClient = createPublicClient({
  chain: metanodeChain,
  transport: http(),
});

const walletClient = createWalletClient({
  account,
  chain: metanodeChain,
  transport: http(),
});

// Đọc ABI và Bytecode
const abiPath = path.join(process.cwd(), 'build', 'SimpleStorage_sol_SimpleStorage.abi');
const binPath = path.join(process.cwd(), 'build', 'SimpleStorage_sol_SimpleStorage.bin');
const abi = JSON.parse(fs.readFileSync(abiPath, 'utf8'));
const bytecode = `0x${fs.readFileSync(binPath, 'utf8').trim()}` as `0x${string}`;

async function runTests() {
  console.log(`🚀 Bắt đầu test DEPLOY CONTRACT qua Viem...`);
  console.log(`👤 Wallet Address: ${account.address}`);
  console.log("-------------------------------------------------");
  
  let successCount = 0;
  let failCount = 0;
  let contractAddress: `0x${string}` | undefined;

  const runTest = async (name: string, fn: () => Promise<any>) => {
    try {
      const result = await fn();
      console.log(`✅ [PASS] ${name}`);
      console.log(`   Result:`, typeof result === 'bigint' ? result.toString() : result);
      successCount++;
    } catch (error: any) {
      console.error(`❌ [FAIL] ${name}`);
      console.error(`   Error:`, error.message || error);
      failCount++;
    }
    console.log("-------------------------------------------------");
  };

  // 1. Deploy Contract
  await runTest("Deploy SimpleStorage Contract (deployContract)", async () => {
    console.log(`   Đang gửi giao dịch Deploy...`);
    const hash = await walletClient.deployContract({
      abi,
      bytecode,
      args: [],
    });
    
    console.log(`   Tx Hash: ${hash}`);
    console.log(`   Đang chờ mạng đóng Block (Receipt)...`);
    
    const receipt = await publicClient.waitForTransactionReceipt({ hash });
    contractAddress = receipt.contractAddress as `0x${string}`;
    return `Deployed at: ${contractAddress}, Status: ${receipt.status}, Gas Used: ${receipt.gasUsed}`;
  });

  if (!contractAddress) {
    console.log("❌ Không thể tiếp tục test vì Deploy thất bại.");
    return;
  }

  // 2. Read state ban đầu
  await runTest("readContract: Đọc biến value ban đầu", async () => {
    const value = await publicClient.readContract({
      address: contractAddress!,
      abi,
      functionName: 'value',
    });
    return `Value: ${value}`;
  });

  // 3. Ghi dữ liệu vào Contract
  const newValue = 9999n;
  await runTest(`writeContract: Cập nhật biến value thành ${newValue}`, async () => {
    console.log(`   Đang gửi transaction setValue(${newValue})...`);
    // Sử dụng simulateContract để giả lập phí trước (dùng eth_estimateGas, eth_call)
    const { request } = await publicClient.simulateContract({
      address: contractAddress!,
      abi,
      functionName: 'setValue',
      args: [newValue],
      account,
    });
    const hash = await walletClient.writeContract(request);
    
    console.log(`   Tx Hash: ${hash}`);
    console.log(`   Đang chờ mạng đóng Block (Receipt)...`);
    
    const receipt = await publicClient.waitForTransactionReceipt({ hash });
    return `Status: ${receipt.status}, Gas Used: ${receipt.gasUsed}`;
  });

  // 4. Read state lại
  await runTest("readContract: Kiểm tra lại biến value", async () => {
    const value = await publicClient.readContract({
      address: contractAddress!,
      abi,
      functionName: 'value',
    });
    if (value !== newValue) throw new Error(`Lỗi: Value mong đợi là ${newValue} nhưng nhận được ${value}`);
    return `Value đã cập nhật thành công: ${value}`;
  });

  // 5. Kiểm tra Event Logs
  await runTest("getLogs: Kiểm tra Event ValueChanged", async () => {
    const logs = await publicClient.getLogs({
      address: contractAddress!,
      events: abi.filter((x: any) => x.type === 'event'),
      fromBlock: 'earliest',
      toBlock: 'latest',
    });
    return `Đã tìm thấy ${logs.length} logs! Log mới nhất thuộc block: ${logs[logs.length-1]?.blockNumber}`;
  });

  console.log(`🎯 TỔNG KẾT: ${successCount} PASS, ${failCount} FAIL`);
}

runTests().catch(console.error);
