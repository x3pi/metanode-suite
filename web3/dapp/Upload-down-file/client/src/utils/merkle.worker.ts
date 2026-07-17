import { keccak256 } from "viem";

self.onmessage = async (e: MessageEvent) => {
  const { file, CHUNK_SIZE } = e.data;
  
  try {
    const numLeaves = Math.ceil(file.size / CHUNK_SIZE);
    const leaves: Uint8Array[] = new Array(numLeaves);
    
    // Process chunks to build leaves
    for (let i = 0; i < numLeaves; i++) {
      const start = i * CHUNK_SIZE;
      const end = Math.min(start + CHUNK_SIZE, file.size);
      
      // Read only the necessary chunk into memory
      const chunkBlob = file.slice(start, end);
      const chunkBuffer = await chunkBlob.arrayBuffer();
      const chunkData = new Uint8Array(chunkBuffer);
      
      // viem keccak256 accepts ByteArray directly, which is very fast
      const hashHex = keccak256(chunkData);
      
      // Convert hex hash string back to Uint8Array for Merkle tree builder
      const cleanHex = hashHex.startsWith("0x") ? hashHex.slice(2) : hashHex;
      const bytes = new Uint8Array(cleanHex.length / 2);
      for (let j = 0; j < cleanHex.length; j += 2) {
        bytes[j / 2] = parseInt(cleanHex.substring(j, j + 2), 16);
      }
      leaves[i] = bytes;
      
      // Send progress to UI every 50 chunks (50MB) or at the end
      if (i % 50 === 0 || i === numLeaves - 1) {
        self.postMessage({ type: "progress", progress: Math.floor((i / numLeaves) * 100) });
      }
    }
    
    // Return all computed leaves
    self.postMessage({ type: "done", leaves });
  } catch (error: any) {
    self.postMessage({ type: "error", error: error.message });
  }
};
