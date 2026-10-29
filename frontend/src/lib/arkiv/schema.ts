export const ARKIV_CHAIN_ID = 7738577;
export const ARKIV_RPC_URL = "https://rpc.tiramisu.db-chain.testnet.arkiv.network";
export const ARC_CHAIN_ID = 5042002;
export const ARC_ASSET = "0x3600000000000000000000000000000000000000";
export const OFFER_PROJECT = "agentmesh-mumbai";
export const OFFER_KIND = "service-offer-v1";
export const DISCOVERY_QUERY =
  "project = str('agentmesh-mumbai') AND kind = str('service-offer-v1') AND settlement_chain = u64(5042002) AND status = str('active')";

export type InputSchema = {
  type: "object";
  properties: Record<string, { type: "string" | "integer" | "number" | "boolean" }>;
  required: string[];
  additionalProperties: false;
};

export type OfferPayload = {
  endpoint: string;
  method: "POST";
  payee: `0x${string}`;
  input_schema: InputSchema;
};

export type OfferDraft = {
  capability: string;
  priceMicros: string;
  version: string;
  validUntil: string;
  expiresAtBlock: string;
  payload: OfferPayload;
};

export type OfferErrorCode =
  | "unconfigured"
  | "disconnected"
  | "wrong-network"
  | "account-changed"
  | "invalid-offer"
  | "endpoint-rejected"
  | "wallet-error"
  | "write-rejected"
  | "write-uncertain";

export class OfferWriteError extends Error {
  constructor(
    public readonly code: OfferErrorCode,
    message: string,
    public readonly transactionHash?: `0x${string}`,
  ) {
    super(message);
    this.name = "OfferWriteError";
  }
}

function invalid(): never {
  throw new OfferWriteError("invalid-offer", "The public offer has invalid or unsupported fields.");
}

function exactObject(value: unknown, keys: string[]): asserts value is Record<string, unknown> {
  if (
    value === null || typeof value !== "object" || Array.isArray(value) ||
    ![null, Object.prototype].includes(Object.getPrototypeOf(value)) ||
    Object.keys(value).length !== keys.length ||
    Object.keys(value).some((key) => !keys.includes(key))
  ) invalid();
}

export function isAddress(value: unknown): value is `0x${string}` {
  return typeof value === "string" && /^0x[0-9a-fA-F]{40}$/.test(value) &&
    value !== "0x0000000000000000000000000000000000000000";
}

export function isHash(value: unknown): value is `0x${string}` {
  return typeof value === "string" && /^0x[0-9a-fA-F]{64}$/.test(value);
}

export function positiveU64(value: unknown): bigint {
  if (typeof value !== "string" || !/^[1-9][0-9]{0,19}$/.test(value)) invalid();
  const parsed = BigInt(value);
  if (parsed > 18446744073709551615n) invalid();
  return parsed;
}

export function isHTTPSURL(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 2048) return false;
  try {
    const u = new URL(value);
    return u.protocol === "https:" && u.hostname !== "" && !u.username && !u.password &&
      !u.search && !u.hash && !value.includes("?") && !value.includes("#");
  } catch {
    return false;
  }
}

// Only schema structure is copied. User values, examples and credentials cannot
// enter the public payload through an extra property on the caller's object.
export function prepareOffer(draft: OfferDraft, owner: string, nowSeconds: number) {
  exactObject(draft, ["capability", "priceMicros", "version", "validUntil", "expiresAtBlock", "payload"]);
  if (typeof draft.capability !== "string" || !/^[a-z][a-z0-9_.-]{0,63}$/.test(draft.capability)) invalid();
  const priceMicros = positiveU64(draft.priceMicros);
  const version = positiveU64(draft.version);
  const validUntil = positiveU64(draft.validUntil);
  const expiresAtBlock = positiveU64(draft.expiresAtBlock);
  if (!Number.isSafeInteger(nowSeconds) || nowSeconds < 0 || validUntil <= BigInt(nowSeconds) || validUntil > 253402300799n) invalid();
  exactObject(draft.payload, ["endpoint", "method", "payee", "input_schema"]);
  const body = draft.payload;
  if (!isHTTPSURL(body.endpoint) || body.method !== "POST" || !isAddress(owner) || !isAddress(body.payee) || body.payee.toLowerCase() !== owner.toLowerCase()) invalid();
  exactObject(body.input_schema, ["type", "properties", "required", "additionalProperties"]);
  const schema = body.input_schema;
  if (schema.type !== "object" || schema.additionalProperties !== false || !schema.properties || Array.isArray(schema.properties) || typeof schema.properties !== "object" || ![null, Object.prototype].includes(Object.getPrototypeOf(schema.properties)) || !Array.isArray(schema.required)) invalid();
  const properties: InputSchema["properties"] = Object.create(null);
  const fields = Object.entries(schema.properties);
  if (fields.length > 32) invalid();
  for (const [name, field] of fields) {
    if (!/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(name) || name === "constructor" || name === "prototype") invalid();
    exactObject(field, ["type"]);
    if (!["string", "integer", "number", "boolean"].includes(field.type)) invalid();
    properties[name] = { type: field.type };
  }
  if (schema.required.some((name) => typeof name !== "string" || !Object.hasOwn(properties, name)) || new Set(schema.required).size !== schema.required.length) invalid();
  const payload: OfferPayload = {
    endpoint: body.endpoint,
    method: "POST",
    payee: body.payee.toLowerCase() as `0x${string}`,
    input_schema: { type: "object", properties, required: [...schema.required], additionalProperties: false },
  };
  if (new TextEncoder().encode(JSON.stringify(payload)).length > 16 * 1024) invalid();
  return { capability: draft.capability, priceMicros, version, validUntil, expiresAtBlock, payload };
}
