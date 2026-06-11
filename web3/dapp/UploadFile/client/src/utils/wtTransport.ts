/**
 * wtTransport.ts
 * Các hàm tiện ích để giao tiếp với WebTransport server (wt_server.rs).
 *
 * Frame format (Request → Server):
 *   [4 byte BE uint32 length][JSON bytes]
 *
 * Frame format (Response ← Server, thành công):
 *   [4 byte BE uint32 length][1 byte ID length][ID bytes][raw chunk bytes]
 *
 * Frame format (Response ← Server, lỗi):
 *   [4 byte BE uint32 length][0xFF][1 byte ID length][ID bytes][error message UTF-8]
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

  if (total < 5) return { ok: false, error: "Response quá ngắn (< 5 bytes)" };

  // Ghép tất cả chunks lại
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    bytes.set(c, offset);
    offset += c.length;
  }

  const payloadLen = new DataView(bytes.buffer).getUint32(0, false);
  const payload = bytes.slice(4, 4 + payloadLen);

  // Error frame: byte đầu tiên là 0xFF
  if (payload[0] === 0xff) {
    const idLen = payload[1];
    const errMsg = new TextDecoder().decode(payload.slice(2 + idLen));
    return { ok: false, error: errMsg };
  }

  const idLen = payload[0];
  const idStr = new TextDecoder().decode(payload.slice(1, 1 + idLen));

  // ArrayBuffer cần copy vì slice trả về view trên cùng buffer
  const rawData = payload.slice(1 + idLen).buffer.slice(
    payload.byteOffset + 1 + idLen,
    payload.byteOffset + payloadLen
  );

  return { ok: true, id: idStr, data: rawData };
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
          0x22, 0x5d, 0x5b, 0xa9, 0x8d, 0x05, 0xd0, 0xb3, 0xb9, 0x8f, 0xbe, 0xb6,
          0xb2, 0x22, 0xb5, 0x36, 0x52, 0x49, 0xe7, 0x78, 0x2b, 0xfe, 0xb8, 0x8b,
          0xfb, 0x37, 0xb8, 0x99, 0xce, 0x4a, 0xbb, 0xc7
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


