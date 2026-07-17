import { createPublicClient, createWalletClient, http, type Hex, decodeEventLog, keccak256, stringToHex } from "viem";
import { privateKeyToAccount, sign } from "viem/accounts";
import { contracts, privateKey } from "../constants/contracts";
import { chain991, DOWNLOAD_SERVER_1, DOWNLOAD_SERVER_2 } from "../constants/customChain";

const publicClient = createPublicClient({
  chain: chain991,
  transport: http(),
});

const account = privateKeyToAccount(`0x${privateKey}` as Hex);
const walletClient = createWalletClient({
  chain: chain991,
  transport: http(),
  account,
});

export async function downloadFileAndSave(fileKey: string, onProgress?: (msg: string) => void): Promise<void> {
  const startTime = Date.now();
  try {
    onProgress?.("Đang lấy thông tin tệp từ blockchain...");
    const fileInfo = await publicClient.readContract({
      address: contracts.File.address as `0x${string}`,
      abi: contracts.File.abi,
      functionName: "getFileInfo",
      args: [fileKey],
    }) as any;

    const totalChunks = Number(fileInfo.totalChunks);
    if (totalChunks === 0) throw new Error("File rỗng hoặc không tồn tại");

    onProgress?.(`File có ${totalChunks} chunks. Đang tính phí...`);
    const requiredPayment = await publicClient.readContract({
      address: contracts.File.address as `0x${string}`,
      abi: contracts.File.abi,
      functionName: "calculatePrice",
      args: [BigInt(totalChunks)],
    }) as bigint;

    onProgress?.("Đang thanh toán để lấy DownloadKey...");
    const hash = await walletClient.writeContract({
      address: contracts.File.address as `0x${string}`,
      abi: contracts.File.abi,
      functionName: "payForDownload",
      args: [fileKey, 1n],
      value: requiredPayment,
      gas: 3000000n,
    });

    const receipt = await publicClient.waitForTransactionReceipt({ hash });

    let downloadKey: string | null = null;
    for (const log of receipt.logs) {
      try {
        const decoded = decodeEventLog({
          abi: contracts.File.abi,
          data: log.data,
          topics: log.topics,
        });
        if (decoded.eventName === "DownloadKeyGenerated") {
          const args = decoded.args as any;
          if (args && args.downloadKey) {
            downloadKey = args.downloadKey;
            break;
          }
        }
      } catch {
        continue;
      }
    }

    if (!downloadKey) {
      throw new Error("Không tìm thấy DownloadKey trong giao dịch thanh toán");
    }

    onProgress?.("Đang tạo chữ ký xác thực...");
    // Rust server yêu cầu: keccak256("0x00" + download_key_without_0x)
    const downloadKeyStr = downloadKey.replace("0x", "");
    const fullMessage = "0x00" + downloadKeyStr;
    const messageHash = keccak256(stringToHex(fullMessage));

    // Ký trực tiếp hash (không kèm prefix Ethereum Signed Message)
    const signatureObj = await sign({
      hash: messageHash,
      privateKey: `0x${privateKey}` as Hex
    });

    // Convert signature object sang Hex string (65 bytes: r + s + v)
    const vValue = signatureObj.v !== undefined
      ? signatureObj.v
      : (signatureObj.yParity === 0 ? 27n : 28n);
    const vHex = vValue.toString(16).padStart(2, '0');
    const signatureHex = `${signatureObj.r}${signatureObj.s.replace('0x', '')}${vHex}`;

    let chunksData = new Array<ArrayBuffer>(totalChunks);

    onProgress?.("Đang khởi tạo kết nối WebTransport...");
    const { getWtOptions, fetchChunkOnStream } = await import('./wtTransport');

    // Mở connection 1 lần duy nhất cho mỗi server
    const t1 = new WebTransport(`${DOWNLOAD_SERVER_1}/quic`, getWtOptions());
    const t2 = new WebTransport(`${DOWNLOAD_SERVER_2}/quic`, getWtOptions());
    await Promise.all([t1.ready, t2.ready]);

    try {
      // Tải song song (Concurrency limit để tránh quá tải)
      // Dùng Worker Pool thực thụ (Xoay vòng liên tục) thay vì Batching
      const CONCURRENCY_LIMIT = 10;
      let currentIndex = 0;
      let hasError = false; // Cờ báo lỗi để dừng các worker khác

      // Định nghĩa 1 Worker
      const worker = async () => {
        while (currentIndex < totalChunks && !hasError) {
          const chunkIndex = currentIndex++; // Lấy task tiếp theo và tăng biến đếm ngay lập tức
          const transport = chunkIndex % 2 === 0 ? t1 : t2;

          let lastError: any;
          const MAX_RETRIES = 3;
          let success = false;

          for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
            try {
              const result = await fetchChunkOnStream(transport, downloadKeyStr, chunkIndex, signatureHex);
              if (!result.ok) throw new Error(`Server báo lỗi: ${result.error}`);

              // Gán trực tiếp vào mảng theo đúng index (không cần sắp xếp lại)
              chunksData[chunkIndex] = result.data;
              success = true;
              break;
            } catch (err: any) {
              lastError = err;
              console.warn(`[Retry ${attempt}/${MAX_RETRIES}] Lỗi tải chunk ${chunkIndex}:`, err);
              if (attempt < MAX_RETRIES) {
                await new Promise(res => setTimeout(res, 1000 * attempt));
              }
            }
          }
          if (!success) {
            hasError = true;
            throw new Error(`Lỗi tải chunk ${chunkIndex} sau ${MAX_RETRIES} lần thử: ${lastError?.message || String(lastError)}`);
          }

          // Cập nhật log 
          if (chunkIndex % 50 === 0) {
            onProgress?.(`Đang tải... đã xong ${chunkIndex}/${totalChunks} chunks`);
          }
        }
      };

      // Khởi chạy 20 workers cùng lúc
      const workers = [];
      for (let i = 0; i < CONCURRENCY_LIMIT; i++) {
        workers.push(worker());
      }

      // Đợi tất cả workers cày xong
      await Promise.all(workers);
    } finally {
      t1.close();
      t2.close();
    }

    onProgress?.("Đang ghép file và lưu xuống máy...");
    const blob = new Blob(chunksData as BlobPart[]);
    const url = window.URL.createObjectURL(blob);

    const a = document.createElement("a");
    a.href = url;
    a.download = fileInfo.name || `downloaded-${fileKey}`;
    document.body.appendChild(a);
    a.click();

    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);

    const endTime = Date.now();
    console.log(`⏱️ Tổng thời gian download: ${((endTime - startTime) / 1000).toFixed(2)}s`);

    onProgress?.("Tải file thành công!");

  } catch (error) {
    console.error(error);
    onProgress?.(`Lỗi: ${error instanceof Error ? error.message : String(error)}`);
    throw error;
  }
}
