import { createPublicClient, createWalletClient, http, type Address, type Hex, decodeEventLog, bytesToHex, keccak256, stringToHex } from "viem";
import { privateKeyToAccount, sign } from "viem/accounts";
import { contracts, privateKey } from "../constants/contracts";
import { chain991, DOWNLOAD_SERVER_1, DOWNLOAD_SERVER_2 } from "../constants/customChain";
import { buildMerkleTreeFromLeaves, getMerkleProofPadded } from "./merkle";
import type { Abi } from "viem";

const CHUNK_SIZE = 1024 * 1024; // 1MB chunks
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

export interface FileInfo {
  owner: Address;
  merkleRoot: Hex;
  contentLen: bigint;
  totalChunks: bigint;
  expireTime: bigint;
  name: string;
  ext: string;
  contentDisposition: string;
  contentID: string;
}

export interface UploadProgress {
  stage: "preparing" | "pushing" | "uploading" | "completed" | "error";
  fileKey?: Hex;
  currentChunk?: number;
  totalChunks?: number;
  error?: string;
}

// Calculate price for upload
export async function calculatePrice(totalChunks: bigint): Promise<bigint> {
  const result = await publicClient.readContract({
    address: contracts.File.address,
    abi: contracts.File.abi as Abi,
    functionName: "calculatePrice",
    args: [totalChunks],
  });
  return result as bigint;
}

// Push file info and get fileKey
export async function pushFileInfo(
  info: Omit<FileInfo, "owner">,
  onProgress?: (progress: UploadProgress) => void
): Promise<Hex> {
  onProgress?.({ stage: "pushing" });

  const fileInfoStruct = {
    owner: account.address,
    merkleRoot: info.merkleRoot,
    contentLen: Number(info.contentLen),
    totalChunks: Number(info.totalChunks),
    expireTime: Number(info.expireTime),
    name: info.name,
    ext: info.ext,
    contentDisposition: info.contentDisposition,
    contentID: info.contentID,
    status: 0,
  };

  const hash = await walletClient.writeContract({
    address: contracts.File.address,
    abi: contracts.File.abi as Abi,
    functionName: "pushFileInfo",
    args: [fileInfoStruct],
    gas: 3000000n,
  });

  const receipt = await publicClient.waitForTransactionReceipt({ hash });

  if (receipt.status !== "success") {
    throw new Error("Transaction failed");
  }

  let fileKey: Hex | null = null;
  for (const log of receipt.logs) {
    try {
      const decoded = decodeEventLog({
        abi: contracts.File.abi as Abi,
        data: log.data,
        topics: log.topics,
      });

      if (decoded.eventName === "FileAdded") {
        const args = decoded.args as unknown;
        if (args && typeof args === "object" && "fileKey" in args) {
          fileKey = args.fileKey as Hex;
          break;
        }
      }
    } catch {
      continue;
    }
  }

  if (!fileKey) {
    throw new Error("FileAdded event not found in transaction receipt");
  }
  onProgress?.({ stage: "uploading", fileKey, currentChunk: 0, totalChunks: Number(info.totalChunks) });

  return fileKey;
}

// Main upload function
export async function uploadFile(
  file: File,
  onProgress?: (progress: UploadProgress) => void
): Promise<Hex> {
  const startTime = Date.now();
  try {
    onProgress?.({ stage: "preparing" });

    // 1. Calculate file info
    const contentLen = BigInt(file.size);
    const totalChunks = BigInt(Math.ceil(file.size / CHUNK_SIZE));
    console.log("totalChunks", totalChunks);

    // 2. Build Merkle tree using Worker to avoid blocking UI and saving RAM
    console.time("⏱️ Băm Merkle Tree (Frontend)");
    const tMerkle = Date.now();
    const leaves = await new Promise<Uint8Array[]>((resolve, reject) => {
      const worker = new Worker(new URL("./merkle.worker.ts", import.meta.url), { type: "module" });
      worker.onmessage = (e) => {
        if (e.data.type === "done") {
          worker.terminate();
          resolve(e.data.leaves);
        } else if (e.data.type === "error") {
          worker.terminate();
          reject(new Error(e.data.error));
        }
      };
      worker.onerror = (err) => {
        worker.terminate();
        reject(err);
      };
      worker.postMessage({ file, CHUNK_SIZE });
    });

    const { paddedLeaves, merkleRoot } = buildMerkleTreeFromLeaves(leaves);
    const merkleRootHex = `0x${Array.from(merkleRoot).map((b) => b.toString(16).padStart(2, "0")).join("")}` as Hex;
    console.timeEnd("⏱️ Băm Merkle Tree (Frontend)");
    console.log(`Băm Merkle xong trong: ${((Date.now() - tMerkle) / 1000).toFixed(2)}s`);

    // 3. Prepare file info
    const fileName = file.name;
    const fileExt = file.name.split(".").pop() || "";
    const expireTime = BigInt(Math.floor(Date.now() / 1000) + 365 * 24 * 60 * 60);

    const info: Omit<FileInfo, "owner"> = {
      merkleRoot: merkleRootHex,
      contentLen,
      totalChunks,
      expireTime,
      name: fileName,
      ext: fileExt,
      contentDisposition: "inline",
      contentID: merkleRootHex,
    };

    // 4. Push file info
    console.time("⏱️ Đợi Smart Contract (PushFileInfo)");
    const tContract = Date.now();
    const fileKey = await pushFileInfo(info, onProgress);
    console.timeEnd("⏱️ Đợi Smart Contract (PushFileInfo)");
    console.log(`Gọi Smart Contract xong trong: ${((Date.now() - tContract) / 1000).toFixed(2)}s`);

    // 5. Create Signature (once for all chunks)
    const fileKeyStr = fileKey.replace("0x", "");
    // Rust hash the string: "0x00" + fileKeyWithout0x + merkleRootHex (with 0x)
    const messageToSign = "0x00" + fileKeyStr + merkleRootHex;
    const messageHash = keccak256(stringToHex(messageToSign));

    // Ký trực tiếp hash (không kèm prefix Ethereum Signed Message) giống như Rust
    const signatureObj = await sign({
      hash: messageHash,
      privateKey: `0x${privateKey}` as Hex
    });

    const vValue = signatureObj.v !== undefined
      ? signatureObj.v
      : (signatureObj.yParity === 0 ? 27n : 28n);
    const vHex = vValue.toString(16).padStart(2, '0');
    const signatureHex = `${signatureObj.r}${signatureObj.s.replace('0x', '')}${vHex}`;

    // 6. Connect WebTransport and upload chunks
    const { getWtOptions, pushChunkOnStream } = await import('./wtTransport');
    
    console.time("⏱️ Mở kết nối WebTransport (QUIC)");
    const tWT = Date.now();
    const t1 = new WebTransport(`${DOWNLOAD_SERVER_1}/quic`, getWtOptions());
    const t2 = new WebTransport(`${DOWNLOAD_SERVER_2}/quic`, getWtOptions());
    await Promise.all([t1.ready, t2.ready]);
    console.timeEnd("⏱️ Mở kết nối WebTransport (QUIC)");
    console.log(`Kết nối QUIC xong trong: ${((Date.now() - tWT) / 1000).toFixed(2)}s`);

    const uploadStartTime = Date.now();
    try {
      const CONCURRENCY_LIMIT = 10; // Match Golang's 5 workers for optimal performance
      let currentIndex = 0;
      let hasError = false;
      let uploadedCount = 0;

      const worker = async () => {
        while (currentIndex < Number(totalChunks) && !hasError) {
          const chunkIndex = currentIndex++;
          const transport = chunkIndex % 2 === 0 ? t1 : t2;

          let success = false;
          let lastError: any = null;
          const MAX_RETRIES = 3;

          const start = chunkIndex * CHUNK_SIZE;
          const end = Math.min(start + CHUNK_SIZE, file.size);
          const chunkBlob = file.slice(start, end);
          const chunkBuffer = await chunkBlob.arrayBuffer();
          const chunkData = new Uint8Array(chunkBuffer);

          const proof = getMerkleProofPadded(paddedLeaves, chunkIndex);
          const proofHex = proof.map((p: Uint8Array) => bytesToHex(p) as string);

          for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
            try {
              const res = await pushChunkOnStream(
                transport,
                contracts.File.address,
                fileKey,
                chunkIndex,
                chunkData,
                signatureHex,
                proofHex,
                merkleRootHex
              );

              if (!res.ok) {
                throw new Error(res.error);
              }

              success = true;
              uploadedCount++;
              onProgress?.({
                stage: "uploading",
                fileKey,
                currentChunk: uploadedCount,
                totalChunks: Number(totalChunks),
              });
              break;
            } catch (e: any) {
              lastError = e;
              console.warn(`[Retry ${attempt}/${MAX_RETRIES}] Chunk ${chunkIndex} upload error:`, e);
              if (attempt < MAX_RETRIES) {
                await new Promise(r => setTimeout(r, 1000 * attempt));
              }
            }
          }
          if (!success) {
            hasError = true;
            throw new Error(`Failed to upload chunk ${chunkIndex}: ${lastError}`);
          }
        }
      };

      const workers = [];
      for (let i = 0; i < CONCURRENCY_LIMIT; i++) {
        workers.push(worker());
      }
      await Promise.all(workers);

    } finally {
      t1.close();
      t2.close();
    }

    const uploadEndTime = Date.now();
    console.log(`⏱️ Thời gian MẠNG (chỉ tính lúc đẩy chunk): ${((uploadEndTime - uploadStartTime) / 1000).toFixed(2)}s`);

    const endTime = Date.now();
    console.log(`⏱️ Tổng thời gian (Hash + SmartContract + Network): ${((endTime - startTime) / 1000).toFixed(2)}s`);

    onProgress?.({ stage: "completed", fileKey, currentChunk: Number(totalChunks), totalChunks: Number(totalChunks) });
    return fileKey;
  } catch (error: unknown) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    onProgress?.({ stage: "error", error: errorMessage });
    throw error;
  }
}

