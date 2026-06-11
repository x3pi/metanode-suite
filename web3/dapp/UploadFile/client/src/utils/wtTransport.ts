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
  return {
    serverCertificateHashes: [
      {
        algorithm: "sha-256",
        value: new Uint8Array([
          0xec, 0x3b, 0x50, 0xed, 0xc4, 0xd0, 0x56, 0xdc, 0xd1, 0xf7, 0xb2, 0x36, 0xdd, 0xa4, 0x3d, 0x1a,
          0xdc, 0x19, 0xcd, 0x38, 0xf1, 0xca, 0xf1, 0xd4, 0x6a, 0x05, 0xa9, 0x49, 0x26, 0x4c, 0xf9, 0x23
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


