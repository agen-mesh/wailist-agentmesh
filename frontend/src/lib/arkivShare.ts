import { createPublicClient } from "@arkiv-network/sdk";
import { rabdi } from "@arkiv-network/sdk/chains";
import { http } from "viem";
import { openShare, ShareKeyError } from "./shareCrypto";
import {
  decodeWorkflowShare,
  ShareExpandError,
  type WorkflowShareData,
} from "./workflowShare";

const gone =
  "This link has died. Shares last 7 days, and a retired Arkiv testnet deletes them. AgentMesh does not keep a copy.";
const unreachable = "Couldn't reach Arkiv, try again.";
const notConfigured = "Sharing is not configured on this build.";

export class ShareGoneError extends Error {
  constructor() {
    super(gone);
    this.name = "ShareGoneError";
  }
}

export class ShareUnreachableError extends Error {
  constructor() {
    super(unreachable);
    this.name = "ShareUnreachableError";
  }
}

export class ShareNotConfiguredError extends Error {
  constructor() {
    super(notConfigured);
    this.name = "ShareNotConfiguredError";
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

const addressPattern = /^0x[0-9a-fA-F]{40}$/;

function expectedCreator(): string | null {
  const raw = process.env.NEXT_PUBLIC_ARKIV_SHARE_ADDRESS ?? "";
  if (!addressPattern.test(raw)) return null;
  return raw.toLowerCase();
}

function creatorOf(entity: unknown): string | null {
  if (!entity || typeof entity !== "object") return null;
  const value = (entity as Record<string, unknown>).$creator;
  if (typeof value !== "string" || !addressPattern.test(value)) return null;
  return value.toLowerCase();
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

function missingEntity(err: unknown): boolean {
  return !!err && typeof err === "object" && (err as { name?: string }).name === "NoEntityFoundError";
}

export async function readShareEntity(entityKey: string): Promise<Uint8Array> {
  const want = expectedCreator();
  if (!want) throw new ShareNotConfiguredError();
  const client = createPublicClient({
    chain: rabdi({ accessKey: process.env.NEXT_PUBLIC_ARKIV_ACCESS_KEY ?? "" }),
    transport: http(),
  });
  let entity: unknown;
  try {
    entity = await client.getEntity(entityKey as `0x${string}`);
  } catch (err) {
    if (missingEntity(err)) throw new ShareGoneError();
    throw new ShareUnreachableError();
  }
  if (creatorOf(entity) !== want) throw new ShareGoneError();
  const bytes = payloadBytes(entity);
  if (!bytes) throw new ShareGoneError();
  return bytes;
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
    if (err instanceof ShareKeyError || err instanceof ShareExpandError) throw err;
    throw new ShareGoneError();
  }
}
