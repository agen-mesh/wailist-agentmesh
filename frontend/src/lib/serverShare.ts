import { createHmac } from "node:crypto";
import { isIP } from "node:net";
import { headers } from "next/headers";
import type { ShareImportRequirements, WorkflowShare } from "./types";

type ShareResponse = {
  share: WorkflowShare;
  requirements: ShareImportRequirements;
};

function canonicalIP(raw: string): string | null {
  if (isIP(raw) === 4) return raw;
  if (isIP(raw) !== 6 || raw.includes("%")) return null;
  const normalized = new URL(`http://[${raw}]`).hostname.slice(1, -1);
  const mapped = /^::ffff:([a-f0-9]+):([a-f0-9]+)$/.exec(normalized);
  if (mapped?.[1] && mapped[2]) {
    const high = Number.parseInt(mapped[1], 16);
    const low = Number.parseInt(mapped[2], 16);
    return [high >>> 8, high & 255, low >>> 8, low & 255].join(".");
  }
  return normalized;
}

export async function loadServerShare(token: string): Promise<ShareResponse | null> {
  // Only Vercel supplies the sanitized visitor header. A header with the same
  // name on a generic Next server is still caller-controlled.
  if (process.env.MOBILE_BUILD === "1" || process.env.VERCEL !== "1") return null;
  const base = process.env.BACKEND_URL;
  const key = process.env.SHARE_READ_RELAY_KEY;
  if (!base || !key || !/^[a-fA-F0-9]{64}$/.test(key)) return null;

  try {
    const origin = new URL(base);
    const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname);
    if (
      (origin.protocol !== "https:" && !(origin.protocol === "http:" && loopback)) ||
      origin.username ||
      origin.password ||
      origin.search ||
      origin.hash ||
      origin.pathname !== "/"
    ) return null;

    const visitor = canonicalIP((await headers()).get("x-vercel-forwarded-for") ?? "");
    if (!visitor) return null;
    const url = new URL(`/shares/${encodeURIComponent(token)}`, origin);
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const payload = ["v1", "GET", url.pathname, visitor, timestamp].join("\n");
    const signature = createHmac("sha256", Buffer.from(key, "hex"))
      .update(payload)
      .digest("hex");
    const response = await fetch(url.href, {
      cache: "no-store",
      redirect: "error",
      headers: {
        "X-AgentMesh-Share-Visitor": visitor,
        "X-AgentMesh-Share-Timestamp": timestamp,
        "X-AgentMesh-Share-Signature": signature,
      },
    });
    if (!response.ok) return null;
    return (await response.json()) as ShareResponse;
  } catch {
    // A relay/configuration failure cannot expose credentials or revive a link.
    return null;
  }
}
