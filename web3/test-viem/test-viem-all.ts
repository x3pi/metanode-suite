import { createPublicClient, createWalletClient, http } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import * as fs from 'fs';
import * as path from 'path';

import { fileURLToPath } from 'url';

// Đọc cấu hình
const configPath = '/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/config-local.json';
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
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
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const abiPath = path.join(__dirname, 'build', 'SimpleStorage_sol_SimpleStorage.abi');
const binPath = path.join(__dirname, 'build', 'SimpleStorage_sol_SimpleStorage.bin');
const abi = JSON.parse(fs.readFileSync(abiPath, 'utf8'));
const bytecode = `0x${fs.readFileSync(binPath, 'utf8').trim()}` as `0x${string}`;

async function runTests() {
  console.log(`🚀 Bắt đầu FULL RPC TEST SUITE qua Viem...`);
  console.log("=================================================");
  
  let successCount = 0;
  let failCount = 0;
  let contractAddress: `0x${string}` | undefined;
  let latestTxHash: `0x${string}` | undefined;

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

  // --- THÔNG TIN MẠNG ---
  console.log("📌 PHẦN 1: THÔNG TIN MẠNG (CHAIN INFO)");
  
  await runTest("eth_chainId", async () => {
    return await publicClient.getChainId();
  });

  await runTest("eth_gasPrice", async () => {
    return await publicClient.getGasPrice();
  });

  // --- THÔNG TIN BLOCK ---
  console.log("📌 PHẦN 2: THÔNG TIN BLOCK (BLOCK INFO)");
  
  let blockNumber = 0n;
  await runTest("eth_blockNumber", async () => {
    blockNumber = await publicClient.getBlockNumber();
    return blockNumber;
  });

  await runTest("eth_getBlockByNumber", async () => {
    const block = await publicClient.getBlock({ blockNumber });
    return `Hash: ${block.hash}, TxCount: ${block.transactions.length}`;
  });

  await runTest("eth_getBlockTransactionCountByNumber", async () => {
    // Note: Viem doesn't have a direct method for this, we use the raw request
    const count = await publicClient.request({
      method: 'eth_getBlockTransactionCountByNumber',
      params: [`0x${blockNumber.toString(16)}`]
    });
    return parseInt(count as string, 16);
  });

  // --- THÔNG TIN TÀI KHOẢN ---
  console.log("📌 PHẦN 3: THÔNG TIN TÀI KHOẢN (ACCOUNT INFO)");

  await runTest("eth_getBalance", async () => {
    return `${await publicClient.getBalance({ address: account.address })} wei`;
  });

  await runTest("eth_getTransactionCount", async () => {
    return await publicClient.getTransactionCount({ address: account.address });
  });

  // --- DEPLOY & GIAO DỊCH ---
  console.log("📌 PHẦN 4: THỰC THI GIAO DỊCH (TRANSACTIONS)");

  await runTest("Deploy Contract (eth_estimateGas + eth_sendRawTransaction)", async () => {
    latestTxHash = await walletClient.deployContract({ abi, bytecode, args: [] });
    const receipt = await publicClient.waitForTransactionReceipt({ hash: latestTxHash });
    contractAddress = receipt.contractAddress as `0x${string}`;
    return `Deployed: ${contractAddress}, Status: ${receipt.status}`;
  });

  if (!contractAddress) return;

  await runTest("eth_getTransactionByHash", async () => {
    const tx = await publicClient.getTransaction({ hash: latestTxHash! });
    return `From: ${tx.from}, To: ${tx.to}, Nonce: ${tx.nonce}`;
  });

  await runTest("eth_getTransactionReceipt", async () => {
    const receipt = await publicClient.getTransactionReceipt({ hash: latestTxHash! });
    return `Status: ${receipt.status}, GasUsed: ${receipt.gasUsed}`;
  });

  await runTest("Write Contract (eth_estimateGas + eth_sendRawTransaction)", async () => {
    const { request } = await publicClient.simulateContract({
      address: contractAddress!,
      abi,
      functionName: 'setValue',
      args: [8888n],
      account,
    });
    latestTxHash = await walletClient.writeContract(request);
    await publicClient.waitForTransactionReceipt({ hash: latestTxHash });
    return `Tx Hash: ${latestTxHash}`;
  });

  // --- TRẠNG THÁI CONTRACT ---
  console.log("📌 PHẦN 5: TRẠNG THÁI CONTRACT (STATE INFO)");

  await runTest("eth_call", async () => {
    return await publicClient.readContract({
      address: contractAddress!,
      abi,
      functionName: 'value',
    });
  });

  await runTest("eth_getCode", async () => {
    const code = await publicClient.getBytecode({ address: contractAddress! });
    return `Length: ${code?.length || 0} bytes`;
  });

  await runTest("eth_getStorageAt", async () => {
    const storage = await publicClient.getStorageAt({
      address: contractAddress!,
      slot: '0x0000000000000000000000000000000000000000000000000000000000000000',
    });
    return storage;
  });

  await runTest("eth_getLogs", async () => {
    const logs = await publicClient.getLogs({
      address: contractAddress!,
      fromBlock: 'earliest',
      toBlock: 'latest',
    });
    return `Found ${logs.length} logs for this contract`;
  });

  // --- TEST LỖI (NEGATIVE TESTS) ---
  console.log("📌 PHẦN 6: TEST KHÔNG TỒN TẠI & BẮT LỖI (NEGATIVE TESTS)");
  
  const dummyAddress = "0x9999999999999999999999999999999999999999";
  const dummyHash = "0x9999999999999999999999999999999999999999999999999999999999999999";

  await runTest("eth_getBlockByNumber (Block không tồn tại)", async () => {
    // Viem ném lỗi BlockNotFoundError nếu trả về null
    try {
      const block = await publicClient.getBlock({ blockNumber: 999999999n });
      return "Sai chuẩn (Không báo lỗi)";
    } catch (e: any) {
      return `Đúng chuẩn (Đã catch lỗi: ${e.name})`;
    }
  });

  await runTest("eth_getTransactionByHash (Tx không tồn tại)", async () => {
    try {
      const tx = await publicClient.getTransaction({ hash: dummyHash });
      return "Sai chuẩn (Không báo lỗi)";
    } catch (e: any) {
      return `Đúng chuẩn (Đã catch lỗi: ${e.name})`;
    }
  });

  await runTest("eth_getTransactionReceipt (Receipt không tồn tại)", async () => {
    try {
      const receipt = await publicClient.getTransactionReceipt({ hash: dummyHash });
      return "Sai chuẩn (Không báo lỗi)";
    } catch (e: any) {
      return `Đúng chuẩn (Đã catch lỗi: ${e.name})`;
    }
  });

  await runTest("eth_getCode (Contract không tồn tại)", async () => {
    const code = await publicClient.getBytecode({ address: dummyAddress });
    return code === undefined ? "Đúng chuẩn (undefined/0x)" : `Sai chuẩn: ${code}`;
  });

  await runTest("eth_call (Gọi hàm trên Contract không tồn tại)", async () => {
    try {
      await publicClient.readContract({
        address: dummyAddress,
        abi,
        functionName: 'value',
        args: [],
      });
      return "Sai chuẩn (Đáng lẽ phải ném lỗi hoặc revert)";
    } catch (error: any) {
      return `Đúng chuẩn (Đã catch được lỗi: ${error.details || error.shortMessage || error.name})`;
    }
  });

  await runTest("eth_getLogs (Contract không tồn tại)", async () => {
    const logs = await publicClient.getLogs({
      address: dummyAddress,
      fromBlock: 'earliest',
      toBlock: 'latest',
    });
    return logs.length === 0 ? "Đúng chuẩn (Trả về mảng rỗng: 0 logs)" : `Sai chuẩn (Tìm thấy ${logs.length} logs)`;
  });

  await runTest("Transaction Revert (Gửi ETH vào Contract không payable)", async () => {
    try {
      await publicClient.estimateGas({
        account: account.address,
        to: contractAddress!,
        value: 1n, // Contract SimpleStorage không nhận native token
      });
      return "Sai chuẩn (Giao dịch không bị revert)";
    } catch (error: any) {
      return `Đúng chuẩn (Đã catch lỗi Revert: ${error.details || error.shortMessage || error.name})`;
    }
  });

  await runTest("Transaction Revert (Gọi hàm setValueRevert với giá trị >= 100)", async () => {
    try {
      const { request } = await publicClient.simulateContract({
        address: contractAddress!,
        abi,
        functionName: 'setValueRevert',
        args: [150n], // Truyền 150 > 100 để test revert
        account,
      });
      // Nếu simulate pass (sai), tiếp tục thử gửi thực tế
      await walletClient.writeContract(request);
      return "Sai chuẩn (Giao dịch không bị revert khi gọi setValueRevert)";
    } catch (error: any) {
      return `Đúng chuẩn (Đã catch lỗi Revert: ${error.details || error.shortMessage || error.name})`;
    }
  });

  await runTest("Lỗi Invalid Nonce (Gửi giao dịch với nonce = 0)", async () => {
    try {
      await walletClient.sendTransaction({
        to: account.address,
        value: 0n,
        nonce: 0, // Cố tình gửi nonce 0 (chắc chắn đã bị xài)
      });
      return "Sai chuẩn (Giao dịch không bị từ chối)";
    } catch (error: any) {
      return `Đúng chuẩn (Đã catch lỗi Nonce: ${error.details || error.shortMessage || error.name})`;
    }
  });

  console.log(`=================================================`);
  console.log(`🎯 TỔNG KẾT: ${successCount} PASS, ${failCount} FAIL`);
}

runTests().catch(console.error);
