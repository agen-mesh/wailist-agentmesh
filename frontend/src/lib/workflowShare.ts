// Codec for the workflow Share/Import round trip. Plain base64 of the JSON
// would make the payload ~33% BIGGER (4 chars per 3 bytes) -- it's an
// encoding, not a compression. Node/edge JSON is repetitive (the same key
// names -- "type", "id", "template", "x", "y" -- repeat across every node),
// which gzip exploits well: gzip-then-base64 nets out smaller than raw
// minified JSON despite the base64 overhead. Measured on the repo's sample
// workflows: ~40-60% smaller than minified JSON, depending on node count.
//
// Uses the native CompressionStream/DecompressionStream Web APIs (Chrome 80+,
// Firefox 113+, Safari 16.4+) instead of pulling in a gzip library.

export interface WorkflowShareData {
  name?: string;
  nodes: unknown[];
  edges: unknown[];
}

// A 128 KB gzip of zeros expands far past anything a workflow graph is.
// Stop reading once the uncompressed bytes pass this cap.
export const maxUncompressedShareBytes = 4 * 1024 * 1024;

export class ShareExpandError extends Error {
  constructor() {
    super("This share is too large to open.");
    this.name = "ShareExpandError";
  }
}

export async function gzipWorkflowShare(
  payload: WorkflowShareData,
): Promise<Uint8Array> {
  const bytes = new TextEncoder().encode(JSON.stringify(payload));
  return gzip(bytes);
}

export async function encodeWorkflowShare(
  payload: WorkflowShareData,
): Promise<string> {
  const compressed = await gzipWorkflowShare(payload);
  return bytesToBase64(compressed);
}

export async function decodeWorkflowShare(
  code: string,
): Promise<WorkflowShareData> {
  const compressed = base64ToBytes(code.trim());
  const bytes = await gunzip(compressed);
  const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
  if (
    !parsed ||
    typeof parsed !== "object" ||
    !Array.isArray((parsed as WorkflowShareData).nodes) ||
    !Array.isArray((parsed as WorkflowShareData).edges)
  ) {
    throw new Error("not a valid workflow share code");
  }
  return parsed as WorkflowShareData;
}

async function gzip(bytes: Uint8Array): Promise<Uint8Array> {
  if (typeof CompressionStream === "undefined") {
    throw new Error(
      "this browser doesn't support compression -- try a recent Chrome, Firefox, or Safari",
    );
  }
  const stream = new CompressionStream("gzip");
  const writer = stream.writable.getWriter();
  // TS's DOM lib types Uint8Array's backing buffer as ArrayBufferLike (which
  // includes SharedArrayBuffer), but WritableStream<BufferSource>.write()
  // wants a concrete ArrayBuffer -- these bytes always come from
  // TextEncoder/atob, never a shared buffer, so the cast is safe.
  void writer.write(bytes as BufferSource);
  void writer.close();
  return new Uint8Array(await new Response(stream.readable).arrayBuffer());
}

async function gunzip(bytes: Uint8Array): Promise<Uint8Array> {
  if (typeof DecompressionStream === "undefined") {
    throw new Error(
      "this browser doesn't support decompression -- try a recent Chrome, Firefox, or Safari",
    );
  }
  const stream = new DecompressionStream("gzip");
  const writer = stream.writable.getWriter();
  const reader = stream.readable.getReader();
  // TS's DOM lib types Uint8Array's backing buffer as ArrayBufferLike (which
  // includes SharedArrayBuffer), but WritableStream<BufferSource>.write()
  // wants a concrete ArrayBuffer -- these bytes always come from
  // TextEncoder/atob, never a shared buffer, so the cast is safe.
  // Read while the write runs. A payload that is not gzip rejects here;
  // discarding that rejection leaves an unhandled stream error.
  let expandError: ShareExpandError | null = null;
  const readAll = (async () => {
    const chunks: Uint8Array[] = [];
    let total = 0;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxUncompressedShareBytes) {
        expandError = new ShareExpandError();
        await reader.cancel().catch(() => {});
        throw expandError;
      }
      chunks.push(value);
    }
    const out = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      out.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return out;
  })();
  try {
    await writer.write(bytes as BufferSource);
    await writer.close();
  } catch (err) {
    await writer.abort(err).catch(() => {});
    await reader.cancel().catch(() => {});
    await readAll.catch(() => {});
    if (expandError) throw expandError;
    throw err;
  }
  try {
    return await readAll;
  } catch (err) {
    if (expandError) throw expandError;
    throw err;
  }
}

// String.fromCharCode(...bytes) blows the call stack on large arrays, so this
// walks the array in chunks well under any engine's argument-count limit.
function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
