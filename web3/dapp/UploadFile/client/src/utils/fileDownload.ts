import { createPublicClient, createWalletClient, http, type Hex, decodeEventLog, keccak256, stringToHex } from "viem";
import { privateKeyToAccount, sign } from "viem/accounts";
import { contracts, privateKey } from "../constants/contracts";
import { chain991, DOWNLOAD_SERVER_1, DOWNLOAD_SERVER_2 } from "../constants/customChain";
import { fetchChunkViaWt } from "./wtTransport";

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

export async function downloadFileAndSave(fileKey: string, onProgress?: (msg: string) => void) {
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

    const chunksData: ArrayBuffer[] = [];

    for (let i = 0; i < totalChunks; i++) {
      onProgress?.(`Đang tải chunk ${i + 1}/${totalChunks}...`);

      // Chunk chẵn → Server 1, chunk lẻ → Server 2
      const serverUrl = i % 2 === 0 ? DOWNLOAD_SERVER_1 : DOWNLOAD_SERVER_2;

      const result = await fetchChunkViaWt(serverUrl, downloadKeyStr, i, signatureHex);

      if (!result.ok) {
        throw new Error(`Lỗi tải chunk ${i} từ ${serverUrl}: ${result.error}`);
      }

      chunksData.push(result.data);
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
    onProgress?.("Tải file thành công!");

  } catch (error) {
    console.error(error);
    onProgress?.(`Lỗi: ${error instanceof Error ? error.message : String(error)}`);
  }
}
