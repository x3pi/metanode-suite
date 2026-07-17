import { createWalletClient, http, type Hex, bytesToHex, hexToBytes, type Address, encodeFunctionData } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { contracts, privateKey } from "../constants/contracts";
import { chain991, GO_BACKEND_RPC_URL } from "../constants/customChain";

const account = privateKeyToAccount(`0x${privateKey}` as Hex);
const walletClient = createWalletClient({
  chain: chain991,
  transport: http(GO_BACKEND_RPC_URL, { batch: false }),
  account,
});

self.onmessage = async (e: MessageEvent) => {
  const { id, fileKey, chunkData, chunkIndex, merkleProof } = e.data;

  const maxRetries = 3;
  let attempt = 0;
  let success = false;
  let errorMsg = "";

  while (attempt < maxRetries && !success) {
    try {
      const dataHex = encodeFunctionData({
        abi: contracts.File.abi,
        functionName: "uploadChunk",
        args: [
          fileKey as Hex,
          bytesToHex(chunkData),
          BigInt(chunkIndex),
          merkleProof.map((p: Uint8Array) => bytesToHex(p) as Hex)
        ]
      });
      // -------------------------------

      // 2. Sign transaction locally (Bypass viem's RPC calls for nonce/gas)
      const signedTxHex = await account.signTransaction({
        to: contracts.File.address as Address,
        data: dataHex,
        gas: 3000000n,
        nonce: 0,
        chainId: chain991.id,
        maxFeePerGas: 0n,
        maxPriorityFeePerGas: 0n,
        value: 0n,
      });

      // 3. POST JSON-RPC directly to HTTP endpoint
      let fetchUrl = GO_BACKEND_RPC_URL;
      
      const rpcPayload = {
        jsonrpc: "2.0",
        method: "eth_sendRawTransaction",
        params: [signedTxHex],
        id: id,
      };

      const response = await fetch(fetchUrl, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(rpcPayload),
      });

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const respJson = await response.json();
      if (respJson.error) {
        throw new Error(respJson.error.message || "Unknown RPC error");
      }
      const hash = respJson.result;

      success = true;
      self.postMessage({ id, success: true, hash });
    } catch (err: any) {
      attempt++;
      errorMsg = err?.message || String(err);
      if (attempt < maxRetries) {
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
    }
  }

  if (!success) {
    self.postMessage({ id, success: false, error: errorMsg });
  }
};
