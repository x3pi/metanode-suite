import { createPublicClient, createWalletClient, http, type Address, type Hex, decodeEventLog } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { contracts, privateKey } from "../constants/contracts";
import { chain991 } from "../constants/customChain";
import { buildMerkleTreePadded, getMerkleProofPadded } from "./merkle";
import type { Abi } from "viem";

const CHUNK_SIZE = 250 * 1024; // 1MB chunks (adjust as needed)

// Create clients
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

  const requiredPayment = await calculatePrice(info.totalChunks);
  console.log(`Required payment: ${requiredPayment.toString()} wei`);

  // Ensure all BigInt values are properly converted and status is included
  const fileInfoStruct = {
    owner: account.address,
    merkleRoot: info.merkleRoot,
    contentLen: Number(info.contentLen), // Convert to number for uint64
    totalChunks: Number(info.totalChunks), // Convert to number for uint64
    expireTime: Number(info.expireTime), // Convert to number for uint64
    name: info.name,
    ext: info.ext,
    contentDisposition: info.contentDisposition,
    contentID: info.contentID,
    status: 0, // FileStatus enum: 0 = Pending (default status for new files)
  };

  console.log("Pushing file info:", fileInfoStruct);
  console.log("Required payment:", requiredPayment?.toString());

  if (!requiredPayment || requiredPayment === undefined) {
    throw new Error("Failed to calculate price: requiredPayment is undefined");
  }

  const hash = await walletClient.writeContract({
    address: contracts.File.address,
    abi: contracts.File.abi as Abi,
    functionName: "pushFileInfo",
    args: [fileInfoStruct],
    value: requiredPayment,
    gas: 3000000n,
  });

  // Wait for transaction receipt
  const receipt = await publicClient.waitForTransactionReceipt({ hash });

  if (receipt.status !== "success") {
    throw new Error("Transaction failed");
  }

  // Extract fileKey from FileAdded event
  // FileAdded event signature: FileAdded(bytes32 fileKey, string name, uint64 contentLen)
  // Event topic[0] is keccak256("FileAdded(bytes32,string,uint64)")
  let fileKey: Hex | null = null;

  for (const log of receipt.logs) {
    try {
      const decoded = decodeEventLog({
        abi: contracts.File.abi as Abi,
        data: log.data,
        topics: log.topics,
      });

      if (decoded.eventName === "FileAdded") {
        // decoded.args can be an object or array depending on event structure
        const args = decoded.args as unknown;
        if (args && typeof args === "object" && "fileKey" in args) {
          fileKey = args.fileKey as Hex;
          break;
        }
      }
    } catch {
      // Not the event we're looking for, continue
      continue;
    }
  }

  if (!fileKey) {
    throw new Error("FileAdded event not found in transaction receipt");
  }
  onProgress?.({ stage: "uploading", fileKey, currentChunk: 0, totalChunks: Number(info.totalChunks) });

  return fileKey;
}

// Upload a single chunk
async function uploadChunk(
  fileKey: Hex,
  chunkData: Uint8Array,
  chunkIndex: number,
  merkleProof: Uint8Array[]
): Promise<void> {
  const maxRetries = 3;
  const retryDelay = 1000; // 1 second

  // Convert merkleProof to bytes32[] format (each proof element must be exactly 32 bytes)
  const merkleProofBytes32 = merkleProof.map((p) => {
    // Ensure each proof is exactly 32 bytes
    const proof32 = new Uint8Array(32);
    proof32.set(p.slice(0, 32), 0);
    return `0x${Array.from(proof32).map((b) => b.toString(16).padStart(2, "0")).join("")}` as Hex;
  });

  // Convert chunkData to hex string for bytes type
  const chunkDataHex = `0x${Array.from(chunkData).map((b) => b.toString(16).padStart(2, "0")).join("")}` as Hex;

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      await walletClient.writeContract({
        address: contracts.File.address,
        abi: contracts.File.abi as Abi,
        functionName: "uploadChunk",
        args: [
          fileKey,
          chunkDataHex,
          BigInt(chunkIndex),
          merkleProofBytes32,
        ],
        gas: 3000000n,
      });

      // await publicClient.waitForTransactionReceipt({ hash });
      return; // Success
    } catch (error: unknown) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      if (errorMessage.includes("to store chunk on disk")) {
        return; // Server error, skip this chunk
      }

      if (attempt < maxRetries) {
        console.warn(`Chunk ${chunkIndex}, attempt ${attempt}/${maxRetries} failed:`, error);
        await new Promise((resolve) => setTimeout(resolve, retryDelay));
      } else {
        throw new Error(`Failed to upload chunk ${chunkIndex} after ${maxRetries} attempts: ${errorMessage}`);
      }
    }
  }
}

// Main upload function
export async function uploadFile(
  file: File,
  onProgress?: (progress: UploadProgress) => void
): Promise<Hex> {
  const startTime = Date.now();
  try {
    onProgress?.({ stage: "preparing" });

    // 1. Read file and split into chunks
    const fileData = new Uint8Array(await file.arrayBuffer());
    const contentLen = BigInt(fileData.length);
    const totalChunks = BigInt(Math.ceil(fileData.length / CHUNK_SIZE));
    console.log("totalChunks", totalChunks);
    // 2. Build Merkle tree
    const chunks: Uint8Array[] = [];
    for (let i = 0; i < totalChunks; i++) {
      const start = Number(i) * CHUNK_SIZE;
      const end = Math.min(start + CHUNK_SIZE, fileData.length);
      chunks.push(fileData.slice(start, end));
    }

    const { paddedLeaves, merkleRoot } = buildMerkleTreePadded(chunks);
    const merkleRootHex = `0x${Array.from(merkleRoot).map((b) => b.toString(16).padStart(2, "0")).join("")}` as Hex;

    // 3. Prepare file info
    const fileName = file.name;
    const fileExt = file.name.split(".").pop() || "";
    const expireTime = BigInt(Math.floor(Date.now() / 1000) + 365 * 24 * 60 * 60); // 1 year

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
    const fileKey = await pushFileInfo(info, onProgress);

    // 5. Upload chunks in parallel (with concurrency limit)
    const concurrencyLimit = 50;
    let currentChunkIndex = 0;

    const uploadChunkWithIndex = async (index: number) => {
      const chunk = chunks[index];
      const proof = getMerkleProofPadded(paddedLeaves, index);
      await uploadChunk(fileKey, chunk, index, proof);
      currentChunkIndex++;
      onProgress?.({
        stage: "uploading",
        fileKey,
        currentChunk: currentChunkIndex,
        totalChunks: Number(totalChunks),
      });
    };

    // Process chunks in batches
    for (let i = 0; i < chunks.length; i += concurrencyLimit) {
      const batch = chunks.slice(i, i + concurrencyLimit);
      const batchPromises = batch.map((_, batchIndex) =>
        uploadChunkWithIndex(i + batchIndex)
      );
      await Promise.all(batchPromises);
    }

    const endTime = Date.now();
    console.log(`⏱️ Tổng thời gian upload: ${((endTime - startTime) / 1000).toFixed(2)}s`);

    onProgress?.({ stage: "completed", fileKey, currentChunk: Number(totalChunks), totalChunks: Number(totalChunks) });
    return fileKey;
  } catch (error: unknown) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    onProgress?.({ stage: "error", error: errorMessage });
    throw error;
  }
}

