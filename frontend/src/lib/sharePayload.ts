import type { CustomParam, WorkflowEdge, WorkflowNode } from "./types";
import {
  gzipWorkflowShare,
  type WorkflowShareData,
} from "./workflowShare";

export const maxShareBytes = 131072;

const tooLarge =
  "This workflow is over 128 KB after secrets and files are removed, so it cannot be shared.";

export class ShareTooLargeError extends Error {
  constructor() {
    super(tooLarge);
    this.name = "ShareTooLargeError";
  }
}

type ShareNode = Omit<WorkflowNode, "x" | "y"> & { x?: number; y?: number };

// Fields required to rebuild the graph. Everything else is dropped, including
// keys, wallets, balances, and connector config. Free-text values that are
// not the prompt itself are blanked below.
const keptNodeFields = [
  "id",
  "type",
  "template",
  "x",
  "y",
  "name",
  "label",
  "icon",
  "sub",
  "custom",
  "systemPrompt",
  "model",
  "keyMode",
  "method",
  "url",
  "endpoint",
  "description",
  "price",
  "unit",
  "asset",
  "provider",
  "priceLive",
  "bodyMode",
  "bodyTemplate",
  "stateOp",
  "stateKey",
  "source",
  "emailTo",
  "emailFrom",
  "emailSubject",
  "emailBody",
  "emailProvider",
  "tendrilAction",
  "tendrilNodeId",
  "tendrilHours",
  "tendrilAmount",
  "tendrilMinBalance",
  "tendrilCoverHours",
] as const;

function stripParams(value: unknown): CustomParam[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.map((param) => {
    const source = param && typeof param === "object" ? (param as Record<string, unknown>) : {};
    const out: CustomParam = {
      name: typeof source.name === "string" ? source.name : "",
      kind: source.kind === "file" ? "file" : "text",
      value: "",
    };
    if (typeof source.fileName === "string") out.fileName = source.fileName;
    if (typeof source.mimeType === "string") out.mimeType = source.mimeType;
    return out;
  });
}

function blankDefaults(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const out: Record<string, string> = {};
  for (const key of Object.keys(value as Record<string, unknown>)) out[key] = "";
  return out;
}

function stripDiscovered(value: unknown): unknown[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.map((item) => {
    if (!item || typeof item !== "object") return item;
    const rest = { ...(item as Record<string, unknown>) };
    delete rest.default;
    return rest;
  });
}

function stripNode(node: unknown): ShareNode {
  const source = node && typeof node === "object" ? (node as Record<string, unknown>) : {};
  const copy: Record<string, unknown> = {};
  for (const key of keptNodeFields) {
    if (key in source) copy[key] = source[key];
  }
  const customParams = stripParams(source.customParams);
  if (customParams) copy.customParams = customParams;
  const paramDefaults = blankDefaults(source.paramDefaults);
  if (paramDefaults) copy.paramDefaults = paramDefaults;
  const discoveredParams = stripDiscovered(source.discoveredParams);
  if (discoveredParams) copy.discoveredParams = discoveredParams;
  if (customParams?.some((param) => param.kind === "file")) copy.bodyTemplate = "";
  return copy as ShareNode;
}

function stripEdge(edge: unknown): WorkflowEdge {
  const source = edge && typeof edge === "object" ? (edge as Record<string, unknown>) : {};
  const out: WorkflowEdge = {
    id: typeof source.id === "string" ? source.id : "",
    from: typeof source.from === "string" ? source.from : "",
    to: typeof source.to === "string" ? source.to : "",
    kind: source.kind === "attach" ? "attach" : "flow",
  };
  if (
    source.toPort === "in" ||
    source.toPort === "out" ||
    source.toPort === "model" ||
    source.toPort === "tools" ||
    source.toPort === "top"
  ) {
    out.toPort = source.toPort;
  }
  return out;
}

export function stripShareGraph(input: {
  name?: string;
  nodes: unknown[];
  edges: unknown[];
}): WorkflowShareData {
  const data: WorkflowShareData = {
    nodes: input.nodes.map((node) => stripNode(node)),
    edges: input.edges.map((edge) => stripEdge(edge)),
  };
  if (input.name) data.name = input.name;
  return data;
}

const urlPattern = /https?:\/\/[^\s"'<>]+/g;

export function publishedShareFields(data: WorkflowShareData): { urls: string[]; emails: string[] } {
  const urls = new Set<string>();
  const emails = new Set<string>();
  for (const node of data.nodes) {
    if (!node || typeof node !== "object") continue;
    const record = node as Record<string, unknown>;
    for (const field of ["url", "endpoint", "bodyTemplate"]) {
      const value = record[field];
      if (typeof value !== "string") continue;
      for (const match of value.match(urlPattern) ?? []) urls.add(match);
    }
    for (const field of ["emailTo", "emailFrom"]) {
      const value = record[field];
      if (typeof value === "string" && value.includes("@")) emails.add(value);
    }
  }
  return { urls: [...urls], emails: [...emails] };
}

export async function prepareSharePayload(input: {
  name?: string;
  nodes: unknown[];
  edges: unknown[];
}): Promise<{ bytes: Uint8Array; data: WorkflowShareData }> {
  const data = stripShareGraph(input);
  const bytes = await gzipWorkflowShare(data);
  if (bytes.length > maxShareBytes) throw new ShareTooLargeError();
  return { bytes, data };
}

export function shareURL(
  origin: string,
  entityKey: string,
  key: string | null,
): string {
  const base = origin.endsWith("/") ? origin.slice(0, -1) : origin;
  const path = `${base}/s/${entityKey}`;
  return key ? `${path}#k=${key}` : path;
}
