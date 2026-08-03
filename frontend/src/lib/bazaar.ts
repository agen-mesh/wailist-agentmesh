import type { WorkflowNode } from "./types";

// Mirrors backend/internal/bazaar.Resource. amountMicros is integer atomic
// USDC (6 decimals) — kept as a number and divided only for display, never
// used for arithmetic that has to round-trip.
export interface BazaarResource {
  id: string;
  url: string;
  method: string;
  description: string;
  merchantId: string;
  network: string;
  testnet: boolean;
  amountMicros: number;
  asset: string;
  payTo: string;
  params: Array<{
    name: string;
    type: string;
    required: boolean;
    description: string;
  }>;
  outputExample?: string;
  settleCount: number;
  lastSeen?: string;
  host: string;
  supported: boolean;
  provider?: string;
}

export interface BazaarPage {
  items: BazaarResource[];
  total: number;
  offset: number;
  limit: number;
  supportedCount: number;
}

// Same convention as lib/api.ts: route through /api in the browser so the
// auth cookie stays same-site.
const _CONFIGURED = process.env.NEXT_PUBLIC_API_URL ?? "";
const BASE =
  _CONFIGURED && typeof window !== "undefined" ? "/api" : _CONFIGURED;

export const bazaar = {
  list: async (opts: {
    offset: number;
    limit: number;
    q?: string;
  }): Promise<BazaarPage> => {
    const qs = new URLSearchParams({
      offset: String(opts.offset),
      limit: String(opts.limit),
    });
    if (opts.q) qs.set("q", opts.q);
    const res = await fetch(`${BASE}/bazaar/resources?${qs}`, {
      credentials: "include",
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error ?? "could not load the catalog");
    return data as BazaarPage;
  },
};

// formatPrice renders atomic USDC (6 decimals) as a plain dollar amount, with
// no trailing zeros — 5000 -> "0.005".
export function formatPrice(amountMicros: number): string {
  return String(amountMicros / 1e6);
}

// resourceToNode turns a catalog entry into the node metadata the canvas drop
// handler expects (CanvasGraph.tsx assigns id/x/y itself).
//
// A supported entry arrives with hand-authored params the user simply fills.
// An unsupported one arrives as a bare endpoint: its params, if any, come from
// the publisher's own catalog metadata and may be incomplete, so the user is
// expected to hit Discover in the Inspector to probe the live challenge.
export function resourceToNode(r: BazaarResource): Partial<WorkflowNode> {
  // Catalog param examples are placeholders, never usable values — seed every
  // field empty and let the description show the example.
  const paramDefaults: Record<string, string> = {};
  for (const p of r.params) paramDefaults[p.name] = "";

  return {
    type: "tool402",
    custom: true,
    icon: "✦",
    name: r.supported && r.provider ? r.provider : r.host,
    sub: `${formatPrice(r.amountMicros)} USDC / call`,
    endpoint: r.url,
    method: r.method,
    description: r.description,
    price: formatPrice(r.amountMicros),
    unit: "call",
    asset: "USDC",
    provider: r.supported && r.provider ? r.provider : r.host,
    // Catalog data is a mirror, not a live quote. Leaving this false keeps the
    // Inspector honest: the price shown came from a cache, and Discover is
    // what confirms it against the endpoint's real 402 challenge.
    priceLive: false,
    discoveredParams: r.params.map((p) => ({
      name: p.name,
      type: p.type,
      required: p.required,
      description: p.description,
    })),
    paramDefaults,
  };
}
