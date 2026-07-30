/**
 * wtTransport.ts
 * Các hàm tiện ích để giao tiếp với WebTransport server (wt_server.rs).
 *
 * Frame format (Request → Server):
 *   [4 byte BE uint32 length][2 byte BE uint16 JSON length][JSON bytes][Optional Raw Chunk Data]
 *
 * Frame format (Response ← Server):
 *   [4 byte BE uint32 length][2 byte BE uint16 JSON length][JSON header bytes][Optional Raw chunk bytes]
 */
import { IS_PRODUCTION } from "../constants/customChain";

export interface WtChunkRequest {
  id: string;
  command: "download_chunk";
  payload: {
    download_key: string;
    chunk_index: number;
    signature: string;
  };
}

export interface WtUploadRequest {
  id: string;
  command: "upload_chunk";
  payload: {
    file_key: string;
    chunk_index: number;
    signature: string;
    merkle_proof_hashes: string[];
    merkle_root: string;
  };
}

export interface WtChunkResponseOk {
  ok: true;
  id: string;
  chunkIndex?: number;
  command?: string;
  data: ArrayBuffer;
}

export interface WtChunkResponseErr {
  ok: false;
  error: string;
}

export type WtChunkResponse = WtChunkResponseOk | WtChunkResponseErr;

/**
 * Encode một object thành frame nhị phân:
 * [4 byte BE uint32 length] [2 byte BE json length] [JSON UTF-8 bytes] [Optional Raw Data]
 */
export function encodeFrame(obj: object, rawData?: Uint8Array): Uint8Array {
  const json = JSON.stringify(obj);
  const jsonBytes = new TextEncoder().encode(json);
  const dataLen = rawData ? rawData.length : 0;
  
  const payloadLen = 2 + jsonBytes.length + dataLen;
  const frame = new Uint8Array(4 + payloadLen);
  const view = new DataView(frame.buffer);
  
  view.setUint32(0, payloadLen, false); // big-endian payload length
  view.setUint16(4, jsonBytes.length, false); // big-endian json length
  
  frame.set(jsonBytes, 6);
  if (rawData) {
    frame.set(rawData, 6 + jsonBytes.length);
  }
  
  return frame;
}

/**
 * Tối ưu hóa cực độ (Zero-copy cho dữ liệu lớn):
 * Encode riêng phần Header (4 byte Payload Len + 2 byte JSON Len + JSON Bytes).
 * Trả về Header buffer để ghi trước, sau đó chunk data (1MB) sẽ được ghi thẳng vào luồng
 * mà không cần phải cấp phát mảng bộ nhớ mới và copy.
 */
export function encodeHeaderOnly(obj: object, dataLen: number): Uint8Array {
  const json = JSON.stringify(obj);
  const jsonBytes = new TextEncoder().encode(json);
  
  const payloadLen = 2 + jsonBytes.length + dataLen;
  const frame = new Uint8Array(4 + 2 + jsonBytes.length);
  const view = new DataView(frame.buffer);
  
  view.setUint32(0, payloadLen, false); // big-endian payload length
  view.setUint16(4, jsonBytes.length, false); // big-endian json length
  
  frame.set(jsonBytes, 6);
  return frame;
}

/**
 * Đọc response frame từ ReadableStream của WebTransport.
 * - Thành công: trả về sha256 (hex) và raw ArrayBuffer
 * - Lỗi:       trả về error message string
 */
export async function readResponseFrame(
  reader: ReadableStreamDefaultReader<Uint8Array>
): Promise<WtChunkResponse> {
  const chunks: Uint8Array[] = [];
  let total = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) {
      chunks.push(value);
      total += value.length;
    }
  }

  if (total < 6) return { ok: false, error: "Response quá ngắn (< 6 bytes)" };

  // Ghép tất cả chunks lại
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    bytes.set(c, offset);
    offset += c.length;
  }

  const payloadLen = new DataView(bytes.buffer).getUint32(0, false);
  if (bytes.length < 4 + payloadLen) return { ok: false, error: "Thiếu dữ liệu" };

  const jsonLen = new DataView(bytes.buffer).getUint16(4, false);
  if (bytes.length < 6 + jsonLen) return { ok: false, error: "JSON header bị cắt cụt" };

  const jsonBytes = bytes.subarray(6, 6 + jsonLen);
  const jsonStr = new TextDecoder().decode(jsonBytes);

  let header: any;
  try {
    header = JSON.parse(jsonStr);
  } catch (e) {
    return { ok: false, error: "Lỗi parse JSON header" };
  }

  if (header.status === "error") {
    return { ok: false, error: header.message || "Unknown server error" };
  }

  // Extract raw chunk directly from the original buffer to avoid multiple copies
  // payload starts at 4, json header ends at 4 + 2 + jsonLen = 6 + jsonLen
  // chunk data ends at 4 + payloadLen
  const rawData = bytes.buffer.slice(bytes.byteOffset + 6 + jsonLen, bytes.byteOffset + 4 + payloadLen);

  return { ok: true, id: header.id, command: header.command, data: rawData };
}


export function getWtOptions() {
  if (IS_PRODUCTION) {
    // Không dùng hash khi ở môi trường thật
    return undefined;
  }

  // Môi trường Local (Self-signed)
  return {
    serverCertificateHashes: [
      {
        algorithm: "sha-256",
        value: new Uint8Array([
          0x5e, 0xc2, 0xee, 0x18, 0x16, 0x17, 0x36, 0xf5, 0xa2, 0xc3, 0xc4, 0xfe,
          0xd4, 0xc9, 0xd0, 0x5b, 0xff, 0x23, 0xc2, 0x47, 0x76, 0xeb, 0x46, 0x86,
          0x58, 0x2a, 0xda, 0x86, 0xd9, 0x13, 0x73, 0xd1
        ]),
      },
    ],
  };
}

export async function fetchChunkOnStream(
  transport: WebTransport,
  downloadKey: string,
  chunkIndex: number,
  signature: string
): Promise<WtChunkResponse> {
  const stream = await transport.createBidirectionalStream();
  const writer = stream.writable.getWriter();
  const reader = stream.readable.getReader();

  try {
    const request: WtChunkRequest = {
      id: crypto.randomUUID(),
      command: "download_chunk",
      payload: { download_key: downloadKey, chunk_index: chunkIndex, signature },
    };

    const frame = encodeFrame(request);
    await writer.write(frame);
    await writer.close();
    writer.releaseLock();

    const result = await readResponseFrame(reader);
    return result;
  } finally {
    reader.releaseLock();
  }
}

export async function fetchChunkViaWt(
  serverUrl: string,
  downloadKey: string,
  chunkIndex: number,
  signature: string
): Promise<WtChunkResponse> {
  const url = `${serverUrl}/quic`;
  const transport = new WebTransport(url, getWtOptions());

  try {
    await transport.ready;
    return await fetchChunkOnStream(transport, downloadKey, chunkIndex, signature);
  } finally {
    transport.close();
  }
}

export async function pushChunkOnStream(
  transport: WebTransport,
  fileKey: string,
  chunkIndex: number,
  chunkData: Uint8Array,
  signature: string,
  merkleProofHashes: string[],
  merkleRoot: string
): Promise<WtChunkResponse> {
  const stream = await transport.createBidirectionalStream();
  const writer = stream.writable.getWriter();
  const reader = stream.readable.getReader();

  try {
    const request: WtUploadRequest = {
      id: crypto.randomUUID(),
      command: "upload_chunk",
      payload: { 
        file_key: fileKey, 
        chunk_index: chunkIndex, 
        signature,
        merkle_proof_hashes: merkleProofHashes,
        merkle_root: merkleRoot
      },
    };

    // Tối ưu Zero-copy: Ghi Header trước, Chunk sau
    const header = encodeHeaderOnly(request, chunkData.length);
    await writer.write(header);
    await writer.write(chunkData); // Ghi thẳng chunk 1MB không tốn RAM copy
    await writer.close();
    writer.releaseLock();

    const result = await readResponseFrame(reader);
    return result;
  } finally {
    reader.releaseLock();
  }
}
