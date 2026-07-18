const fs = require('fs');

const path = "web3/dapp/Upload-down-file/client/src/utils/fileUpload.ts";
let content = fs.readFileSync(path, 'utf8');

// Thêm bytesToHex vào import
if (!content.includes('bytesToHex')) {
    content = content.replace('decodeEventLog } from "viem";', 'decodeEventLog, bytesToHex } from "viem";');
}

// Tối ưu hoá việc chuyển merkleProof sang bytes32
const oldMerkle = `  const merkleProofBytes32 = merkleProof.map((p) => {
    // Ensure each proof is exactly 32 bytes
    const proof32 = new Uint8Array(32);
    proof32.set(p.slice(0, 32), 0);
    return \`0x\${Array.from(proof32).map((b) => b.toString(16).padStart(2, "0")).join("")}\` as Hex;
  });`;

const newMerkle = `  const merkleProofBytes32 = merkleProof.map((p) => {
    const proof32 = new Uint8Array(32);
    proof32.set(p.slice(0, 32), 0);
    return bytesToHex(proof32);
  });`;

content = content.replace(oldMerkle, newMerkle);

// Tối ưu hoá việc chuyển chunkData sang hex
const oldChunk = `  // Convert chunkData to hex string for bytes type
  const chunkDataHex = \`0x\${Array.from(chunkData).map((b) => b.toString(16).padStart(2, "0")).join("")}\` as Hex;`;

const newChunk = `  // Convert chunkData to hex string for bytes type (Optimized with viem's bytesToHex)
  const chunkDataHex = bytesToHex(chunkData);`;

content = content.replace(oldChunk, newChunk);

fs.writeFileSync(path, content);
console.log("Patched fileUpload.ts");
