import type { NodeType, WorkflowEdge, WorkflowNode } from "./types";

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
//
// A code is the OFFLINE half of sharing -- the link is the other half, and is
// what most people will use. This exists for handing a workflow to somebody
// over a channel that mangles URLs, and for a recipient who would rather paste
// text than open a link.
//
// What is encoded here has already been sanitised by the backend: the code is
// built from the graph that POST /workflows/{id}/share returned, not from the
// workflow's own nodes. That is deliberate and load-bearing -- there is one
// sanitiser, it lives on the server, and the client is never trusted to
// reproduce it.

// CODE_PREFIX makes a code self-identifying. The original format was bare
// base64 with no marker, so anything at all that happened to decode was tried
// as a workflow, and a wrong paste failed somewhere deep inside JSON.parse
// with a message about a character position. A prefix lets the wrong thing be
// recognised as the wrong thing immediately.
//
// The number is the format version, not the app's. Bumping it is how a future
// change stops an old client confidently misreading a new payload.
const CODE_PREFIX = "am1.";

// A gzip stream can expand enormously -- a few hundred bytes of crafted input
// decompresses to gigabytes, which is the whole trick behind a zip bomb. The
// decoder stops reading past this, so a hostile code costs a bounded amount of
// memory rather than the tab. 8 MiB is far above any real workflow: the
// backend refuses to store a sanitised graph over 1 MiB.
const MAX_DECOMPRESSED_BYTES = 8 << 20;

// Mirrors the NodeType union in types.ts. Duplicated rather than derived
// because a union is erased at run time and this check has to happen at run
// time -- the payload is a stranger's JSON, and `as WorkflowNode[]` is a
// claim, not a check. The test asserts the two stay in step.
export const SHAREABLE_NODE_TYPES: readonly NodeType[] = [
  "trigger",
  "agent",
  "provider",
  "tool",
  "tool402",
  "action",
  "state",
  "end",
  "tendril",
  "google",
];

export interface WorkflowShareData {
  name?: string;
  // Carried so a code says as much about the workflow as a link does. It was
  // missing at first, which meant the offline half of sharing silently
  // dropped the one sentence explaining what the thing is for.
  description?: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

export async function encodeWorkflowShare(
  payload: WorkflowShareData,
): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify({ v: 1, ...payload }));
  const compressed = await gzip(bytes);
  return CODE_PREFIX + bytesToBase64Url(compressed);
}

export async function decodeWorkflowShare(
  code: string,
): Promise<WorkflowShareData> {
  // ALL whitespace goes, not just the ends. A code is one long unbroken
  // string, and the places it travels through break long strings for a
  // living: mail clients hard-wrap at 78 columns, terminals wrap on paste,
  // and a chat client will insert a newline mid-token without asking.
  // Trimming only the ends left those newlines in the middle, where they
  // reached atob and threw -- surfacing as "that doesn't look like a workflow
  // code" for a code that was perfectly good until somebody emailed it.
  const trimmed = code.replace(/\s+/g, "");
  // A legacy code (pre-versioning) is bare standard base64. Still accepted:
  // codes were handed out before this format existed, and refusing them would
  // break one somebody has already sent.
  const body = trimmed.startsWith(CODE_PREFIX)
    ? trimmed.slice(CODE_PREFIX.length)
    : trimmed;

  let compressed: Uint8Array;
  try {
    compressed = base64ToBytes(body);
  } catch {
    throw new Error("that doesn't look like a workflow code");
  }

  const bytes = await gunzip(compressed);
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(bytes));
  } catch {
    throw new Error("that code is damaged -- ask for a fresh one");
  }
  return validateShareData(parsed);
}

// validateShareData is the boundary between "some JSON" and "a workflow".
//
// The previous version checked only that nodes and edges were arrays, then
// cast. Everything below exists because the thing being validated arrives from
// outside the app entirely: a node with no id produces an unreachable node, an
// unknown type reaches the runner's switch and falls through, and an edge
// pointing at a node that is not there reaches the topological sort as a
// dangling reference.
//
// The backend runs its own allowlist over this on import regardless -- this is
// the half that lets a bad paste say so immediately rather than after a round
// trip.
export function validateShareData(parsed: unknown): WorkflowShareData {
  if (!parsed || typeof parsed !== "object") {
    throw new Error("that doesn't look like a workflow code");
  }
  const data = parsed as Record<string, unknown>;
  if (!Array.isArray(data.nodes) || !Array.isArray(data.edges)) {
    throw new Error("that doesn't look like a workflow code");
  }
  if (data.nodes.length === 0) {
    throw new Error("that code has no nodes in it");
  }

  const ids = new Set<string>();
  for (const raw of data.nodes) {
    if (!raw || typeof raw !== "object") {
      throw new Error("that code has a node in it we can't read");
    }
    const node = raw as Partial<WorkflowNode>;
    if (typeof node.id !== "string" || node.id === "") {
      throw new Error("that code has a node with no id");
    }
    if (ids.has(node.id)) {
      throw new Error("that code has two nodes sharing an id");
    }
    if (!SHAREABLE_NODE_TYPES.includes(node.type as NodeType)) {
      throw new Error("that code has a node type we don't recognise");
    }
    ids.add(node.id);
  }

  for (const raw of data.edges) {
    if (!raw || typeof raw !== "object") {
      throw new Error("that code has a connection in it we can't read");
    }
    const edge = raw as Partial<WorkflowEdge>;
    if (!ids.has(edge.from as string) || !ids.has(edge.to as string)) {
      throw new Error("that code has a connection to a node that isn't in it");
    }
  }

  return {
    name: typeof data.name === "string" ? data.name : undefined,
    // Absent in every code minted before this field existed, so its type is
    // checked rather than assumed -- same treatment as name.
    description:
      typeof data.description === "string" ? data.description : undefined,
    nodes: data.nodes as WorkflowNode[],
    edges: data.edges as WorkflowEdge[],
  };
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
  // Same cast, same reasoning as gzip above.
  //
  // Both promises are caught and dropped, which looks like swallowing an error
  // and is not. When the input is not gzip, BOTH ends of the stream reject
  // with the same failure; the reader below is the one that turns it into a
  // message a person can read. Leaving these two unhandled meant a bad paste
  // -- the single most likely thing to happen to this function -- surfaced as
  // an unhandled rejection (Z_BUF_ERROR) alongside the real error, which a
  // test run reports as a crash even while every assertion passes.
  void writer.write(bytes as BufferSource).catch(() => {});
  void writer.close().catch(() => {});

  // Read incrementally rather than through Response.arrayBuffer(), which would
  // buffer the whole expansion before anyone could object to its size -- the
  // exact thing MAX_DECOMPRESSED_BYTES exists to prevent.
  const reader = stream.readable.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    let result;
    try {
      result = await reader.read();
    } catch {
      throw new Error("that code is damaged -- ask for a fresh one");
    }
    if (result.done) break;
    const chunk = result.value as Uint8Array;
    total += chunk.length;
    if (total > MAX_DECOMPRESSED_BYTES) {
      void reader.cancel();
      throw new Error("that code is too large to be a workflow");
    }
    chunks.push(chunk);
  }

  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
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

// base64url, not base64. Standard base64's "+", "/" and "=" all need escaping
// in a URL, and are mangled by chat and mail clients that helpfully linkify
// text -- which is exactly where a code gets pasted.
function bytesToBase64Url(bytes: Uint8Array): string {
  return bytesToBase64(bytes)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

// Accepts both alphabets, so a legacy code and a new one decode through one
// path. Padding is restored because atob wants it.
function base64ToBytes(b64: string): Uint8Array {
  const normalised = b64.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalised.padEnd(
    normalised.length + ((4 - (normalised.length % 4)) % 4),
    "=",
  );
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
