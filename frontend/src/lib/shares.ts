import { apiFetch, BASE } from "./api";
import { IS_NATIVE } from "./nativeAuth";

const entityKeyPattern = /^0x[0-9a-fA-F]{64}$/;

export async function createShare(
  bytes: Uint8Array,
): Promise<{ entityKey: string; chainId: number }> {
  const res = await apiFetch(`${BASE}/shares`, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: bytes as BufferSource,
  });
  let data: { entityKey?: unknown; chainId?: unknown } = {};
  try {
    data = await res.json();
  } catch {
    data = {};
  }
  const entityKey = typeof data.entityKey === "string" ? data.entityKey : "";
  if (res.status !== 201 || data.chainId !== 7794095 || !entityKeyPattern.test(entityKey)) {
    throw new Error("share_unconfirmed");
  }
  return { entityKey, chainId: 7794095 };
}

export function shareOrigin(): string {
  if (IS_NATIVE) return "https://www.agent-mesh.app";
  const configured = process.env.NEXT_PUBLIC_APP_ORIGIN;
  if (configured) return configured;
  return window.location.origin;
}
