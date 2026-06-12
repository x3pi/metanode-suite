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
  const payload = bytes.slice(4, 4 + payloadLen);

  if (payload.length < 2) return { ok: false, error: "Payload quá ngắn để parse JSON header" };

  const jsonLen = new DataView(payload.buffer, payload.byteOffset).getUint16(0, false);
  if (payload.length < 2 + jsonLen) return { ok: false, error: "JSON header bị cắt cụt" };

  const jsonBytes = payload.slice(2, 2 + jsonLen);
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

  // ArrayBuffer cần copy vì slice trả về view trên cùng buffer
  const rawData = payload.slice(2 + jsonLen).buffer.slice(
    payload.byteOffset + 2 + jsonLen,
    payload.byteOffset + payloadLen
  );

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
          0xe1, 0x34, 0x9a, 0x5b, 0x1e, 0x8f, 0xad, 0xc7, 0x86, 0xa0, 0x67, 0x88,
          0x31, 0xa2, 0x63, 0xc9, 0xb3, 0x98, 0x54, 0x63, 0x41, 0xe5, 0x8e, 0x72,
          0x15, 0x51, 0x75, 0x22, 0x78, 0x3e, 0xf1, 0x2d
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


