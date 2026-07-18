/**
 * wtTransport.ts
 * Các hàm tiện ích để giao tiếp với WebTransport server (wt_server.rs).
 *
 * Frame format (Request → Server):
 *   [4 byte BE uint32 length][JSON bytes]
 *
 * Frame format (Response ← Server):
 *   [4 byte BE uint32 length][2 byte BE uint16 JSON length][JSON header bytes][raw chunk bytes]
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
 * [4 byte BE uint32 length][JSON UTF-8 bytes]
 */
export function encodeFrame(obj: object): Uint8Array {
  const json = JSON.stringify(obj);
  const jsonBytes = new TextEncoder().encode(json);
  const frame = new Uint8Array(4 + jsonBytes.length);
  new DataView(frame.buffer).setUint32(0, jsonBytes.length, false); // big-endian
  frame.set(jsonBytes, 4);
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
          0x3a, 0xd2, 0x0f, 0x88, 0x31, 0xe0, 0xcb, 0xad, 0xec, 0x23, 0xdf, 0x86,
          0xf4, 0xa7, 0x3c, 0x91, 0x74, 0x5b, 0x39, 0x6b, 0xc0, 0x3a, 0x68, 0x76,
          0xc7, 0xb9, 0x0a, 0xc4, 0x51, 0x41, 0xcb, 0x95
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


