import { createPublicClient } from "@arkiv-network/sdk";
import { rabdi } from "@arkiv-network/sdk/chains";
import { http } from "viem";
import { openShare, ShareKeyError } from "./shareCrypto";
import { decodeWorkflowShare, type WorkflowShareData } from "./workflowShare";

const gone =
  "This link has died. Shares last 7 days, and a retired Arkiv testnet deletes them. AgentMesh does not keep a copy.";

export class ShareGoneError extends Error {
  constructor() {
    super(gone);
    this.name = "ShareGoneError";
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

function hexToBytes(value: string): Uint8Array | null {
  if (!/^0x[0-9a-fA-F]*$/.test(value) || value.length % 2 !== 0) return null;
  const hex = value.slice(2);
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function asBytes(value: unknown): Uint8Array | null {
  if (value instanceof Uint8Array) return value;
  if (typeof value === "string") return hexToBytes(value);
  return null;
}

function payloadBytes(entity: unknown): Uint8Array | null {
  if (!entity || typeof entity !== "object") return null;
  const record = entity as Record<string, unknown>;
  const bags = [record.$attributes, record.attributes, record];
  for (const bag of bags) {
    if (!bag || typeof bag !== "object") continue;
    const payload = (bag as Record<string, unknown>).payload;
    if (payload instanceof Uint8Array) return payload;
    if (payload && typeof payload === "object" && "value" in payload) {
      const bytes = asBytes((payload as { value: unknown }).value);
      if (bytes && bytes.byteLength > 0) return bytes;
    }
  }
  return null;
}

export async function readShareEntity(entityKey: string): Promise<Uint8Array> {
  const client = createPublicClient({
    chain: rabdi({ accessKey: process.env.NEXT_PUBLIC_ARKIV_ACCESS_KEY ?? "" }),
    transport: http(),
  });
  try {
    const entity = await client.getEntity(entityKey as `0x${string}`);
    const bytes = payloadBytes(entity);
    if (!bytes) throw new ShareGoneError();
    return bytes;
  } catch (err) {
    if (err instanceof ShareGoneError) throw err;
    throw new ShareGoneError();
  }
}

export async function openSharedWorkflow(
  entityKey: string,
  hash: string,
): Promise<WorkflowShareData> {
  const stored = await readShareEntity(entityKey);
  const params = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
  const fragmentKey = params.get("k");
  const gzipBytes = fragmentKey ? await openShare(stored, fragmentKey) : stored;
  try {
    return await decodeWorkflowShare(bytesToBase64(gzipBytes));
  } catch (err) {
    if (err instanceof ShareKeyError) throw err;
    throw new ShareGoneError();
  }
}
