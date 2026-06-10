/**
 * wtTransport.ts
 * Các hàm tiện ích để giao tiếp với WebTransport server (wt_server.rs).
 *
 * Frame format (Request → Server):
 *   [4 byte BE uint32 length][JSON bytes]
 *
 * Frame format (Response ← Server, thành công):
 *   [4 byte BE uint32 length][32 byte SHA-256][raw chunk bytes]
 *
 * Frame format (Response ← Server, lỗi):
 *   [4 byte BE uint32 length][0xFF][error message UTF-8]
 */

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
  sha256: string;
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
    const errMsg = new TextDecoder().decode(payload.slice(1));
    return { ok: false, error: errMsg };
  }

  if (payloadLen < 32) return { ok: false, error: "Frame không đủ 32 byte SHA-256" };

  const sha256Hex = Array.from(payload.slice(0, 32))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

  // ArrayBuffer cần copy vì slice trả về view trên cùng buffer
  const rawData = payload.slice(32).buffer.slice(
    payload.byteOffset + 32,
    payload.byteOffset + payloadLen
  );

  return { ok: true, sha256: sha256Hex, data: rawData };
}

/**
 * Tải 1 chunk từ WebTransport server.
 * Mở kết nối mới, gửi 1 request, đọc 1 response, đóng kết nối.
 *
 * @param serverUrl - VD: "https://192.168.1.234:8081"
 * @param downloadKey - hex string (không có 0x)
 * @param chunkIndex - số thứ tự chunk
 * @param signature - hex string chữ ký
 */
export async function fetchChunkViaWt(
  serverUrl: string,
  downloadKey: string,
  chunkIndex: number,
  signature: string
): Promise<WtChunkResponse> {
  const url = `${serverUrl}/quic`;
  const transport = new WebTransport(url);

  try {
    await transport.ready;

    const stream = await transport.createBidirectionalStream();
    const writer = stream.writable.getWriter();
    const reader = stream.readable.getReader();

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
    reader.releaseLock();

    return result;
  } finally {
    transport.close();
  }
}

/**
 * Verify SHA-256 của một ArrayBuffer (dùng Web Crypto API).
 * @returns true nếu khớp với expectedHex
 */
export async function verifySha256(data: ArrayBuffer, expectedHex: string): Promise<boolean> {
  const hashBuffer = await crypto.subtle.digest("SHA-256", data);
  const clientHash = Array.from(new Uint8Array(hashBuffer))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
  return clientHash === expectedHex;
}
