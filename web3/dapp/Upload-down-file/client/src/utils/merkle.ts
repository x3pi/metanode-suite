import { keccak256 } from "viem";

// Calculate next power of two
function calculateNextPowerOfTwo(n: number): number {
  if (n <= 1) return 1;
  return Math.pow(2, Math.ceil(Math.log2(n)));
}

// Build Merkle tree with padding (compatible with Go server logic)
export function buildMerkleTreePadded(
  chunks: Uint8Array[]
): { paddedLeaves: Uint8Array[]; merkleRoot: Uint8Array } {
  const numLeaves = chunks.length;
  
  if (numLeaves === 0) {
    const emptyHash = keccak256("0x" as `0x${string}`);
    return { paddedLeaves: [], merkleRoot: hexToBytes(emptyHash) };
  }

  // 1. Pad leaves to next power of 2
  const nextPowerOfTwo = calculateNextPowerOfTwo(numLeaves);
  const leaves: Uint8Array[] = new Array(nextPowerOfTwo);

  // Hash real chunks
  for (let i = 0; i < numLeaves; i++) {
    const hash = keccak256(bytesToHex(chunks[i]));
    leaves[i] = hexToBytes(hash);
  }

  // Pad remaining with empty hash
  const emptyHash = keccak256("0x" as `0x${string}`);
  const emptyHashBytes = hexToBytes(emptyHash);
  for (let i = numLeaves; i < nextPowerOfTwo; i++) {
    leaves[i] = emptyHashBytes;
  }

  // 2. Build tree
  let treeLevel = leaves;
  while (treeLevel.length > 1) {
    const nextLevel: Uint8Array[] = [];
    for (let i = 0; i < treeLevel.length; i += 2) {
      const combined = new Uint8Array(treeLevel[i].length + treeLevel[i + 1].length);
      combined.set(treeLevel[i], 0);
      combined.set(treeLevel[i + 1], treeLevel[i].length);
      const hash = keccak256(bytesToHex(combined));
      nextLevel.push(hexToBytes(hash));
    }
    treeLevel = nextLevel;
  }

  return { paddedLeaves: leaves, merkleRoot: treeLevel[0] };
}

// Get Merkle proof for a leaf in padded tree
export function getMerkleProofPadded(
  paddedLeaves: Uint8Array[],
  chunkIndex: number
): Uint8Array[] {
  const proof: Uint8Array[] = [];
  let treeLevel = paddedLeaves;
  let currentIndex = chunkIndex;

  while (treeLevel.length > 1) {
    const nextLevel: Uint8Array[] = [];
    
    for (let i = 0; i < treeLevel.length; i += 2) {
      let siblingIndex: number | null = null;
      
      if (currentIndex === i) {
        siblingIndex = i + 1; // Right sibling
      } else if (currentIndex === i + 1) {
        siblingIndex = i; // Left sibling
      }

      if (siblingIndex !== null) {
        // This level contains our node, get sibling
        proof.push(treeLevel[siblingIndex]);
      }

      // Calculate parent hash
      const combined = new Uint8Array(treeLevel[i].length + treeLevel[i + 1].length);
      combined.set(treeLevel[i], 0);
      combined.set(treeLevel[i + 1], treeLevel[i].length);
      const hash = keccak256(bytesToHex(combined));
      nextLevel.push(hexToBytes(hash));
    }

    treeLevel = nextLevel;
    currentIndex = Math.floor(currentIndex / 2); // Move up to parent level
  }

  return proof;
}

// Helper: Convert hex string to Uint8Array
function hexToBytes(hex: string): Uint8Array {
  const cleanHex = hex.startsWith("0x") ? hex.slice(2) : hex;
  const bytes = new Uint8Array(cleanHex.length / 2);
  for (let i = 0; i < cleanHex.length; i += 2) {
    bytes[i / 2] = parseInt(cleanHex.substr(i, 2), 16);
  }
  return bytes;
}

// Helper: Convert Uint8Array to hex string
function bytesToHex(bytes: Uint8Array): `0x${string}` {
  return `0x${Array.from(bytes).map((b) => b.toString(16).padStart(2, "0")).join("")}` as `0x${string}`;
}

