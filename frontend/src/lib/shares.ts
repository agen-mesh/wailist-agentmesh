import { apiFetch, BASE } from "./api";
import { IS_NATIVE } from "./nativeAuth";
import { assertWritable } from "./readonly";

const entityKeyPattern = /^0x[0-9a-fA-F]{64}$/;

// Rabdi, the Arkiv testnet. Matches arkivshare.ChainID on the server.
export const shareChainID = 7794095;

const shareCodes = new Set([
  "share_rate",
  "share_budget",
  "share_disabled",
  "share_busy",
  "share_too_large",
]);

export async function createShare(
  bytes: Uint8Array,
): Promise<{ entityKey: string; chainId: number; txHash: string }> {
  assertWritable("POST", "/shares");
  const res = await apiFetch(`${BASE}/shares`, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: bytes as BufferSource,
  });
  let data: { entityKey?: unknown; chainId?: unknown; txHash?: unknown; error?: unknown } = {};
  try {
    data = await res.json();
  } catch {
    data = {};
  }
  const entityKey = typeof data.entityKey === "string" ? data.entityKey : "";
  const txHash = typeof data.txHash === "string" ? data.txHash : "";
  if (
    res.status === 201 &&
    data.chainId === shareChainID &&
    entityKeyPattern.test(entityKey) &&
    entityKeyPattern.test(txHash)
  ) {
    return { entityKey, chainId: shareChainID, txHash };
  }
  const code = typeof data.error === "string" ? data.error : "";
  if (shareCodes.has(code)) throw new Error(code);
  throw new Error("share_unconfirmed");
}

export function shareOrigin(): string {
  if (IS_NATIVE) return "https://www.agent-mesh.app";
  const configured = process.env.NEXT_PUBLIC_APP_ORIGIN;
  if (configured) return configured;
  return window.location.origin;
}
