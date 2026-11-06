import type { WorkflowEdge, WorkflowNode } from "./types";
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

export function stripShareGraph(input: {
  name?: string;
  nodes: ShareNode[];
  edges: WorkflowEdge[];
}): WorkflowShareData {
  const nodes = input.nodes.map((node) => {
    const copy: ShareNode = {
      ...node,
      customParams: node.customParams?.map((param) => ({ ...param })),
    };
    delete copy.apiKey;
    delete copy.emailApiKey;
    delete copy.secrets;
    if (copy.customParams?.some((param) => param.kind === "file")) {
      copy.bodyTemplate = "";
      copy.customParams = copy.customParams.map((param) =>
        param.kind === "file" ? { ...param, value: "" } : param,
      );
    }
    return copy;
  });
  const data: WorkflowShareData = {
    nodes,
    edges: input.edges.map((edge) => ({ ...edge })),
  };
  if (input.name) data.name = input.name;
  return data;
}

export async function prepareSharePayload(input: {
  name?: string;
  nodes: ShareNode[];
  edges: WorkflowEdge[];
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
