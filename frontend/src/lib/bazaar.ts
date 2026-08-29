import { BASE, delay } from "./api";
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

// MOCK_RESOURCES backs list() when NEXT_PUBLIC_API_URL is unset (mock/demo
// mode, e.g. a frontend-only preview deploy) -- every other resource in
// lib/api.ts falls back to fixture data in that mode; this was the one page
// that always hit a relative, hostless URL and rendered the "could not load
// the catalog" error state instead.
//
// The two curated entries mirror backend/internal/bazaar.Curated() exactly
// (same ids, URLs, methods, payTo). Deliberately not invented: a demo user
// can copy an endpoint out of this page, and a fabricated URL or payment
// address is the same hazard as fabricating a curated entry's payment
// params server-side -- which is why Prism is absent from Curated() rather
// than guessed at. Where the real entry has no payTo, this carries "" for
// the same reason. Only settleCount is synthetic (the real value is live
// telemetry with no static counterpart), and the third entry is explicitly
// labelled as a non-real example on an example.com host.
const MOCK_RESOURCES: BazaarResource[] = [
  {
    id: "curated:tendril-run",
    url: "https://tendrilregister.007575.xyz/x402/run",
    method: "POST",
    description:
      "Run a Python script on rented compute and get its stdout back. No lease needed — Tendril picks the machine, runs the job in a throwaway sandbox, and destroys it.",
    merchantId: "",
    network: "algorand-mainnet",
    testnet: false,
    amountMicros: 10000,
    asset: "31566704",
    payTo: "ZIK7QQE7ZX446TW3PN7PQ5UDZNTY7JI5RYNTIU3LPEYBOSTVWI6PTNSWKI",
    params: [
      {
        name: "payload",
        type: "string",
        required: true,
        description: "Python source to execute. Its stdout is returned as `result`.",
      },
    ],
    settleCount: 128,
    host: "tendrilregister.007575.xyz",
    supported: true,
    provider: "Tendril",
  },
  {
    id: "curated:canix-quotes",
    url: "https://canix402-api.compx.io/execution/quotes",
    method: "POST",
    description: "Algorand DeFi execution quotes across supported protocols.",
    merchantId: "",
    network: "algorand-mainnet",
    testnet: false,
    amountMicros: 5000,
    asset: "31566704",
    // The real Curated() entry declares no payTo; don't invent one.
    payTo: "",
    params: [],
    settleCount: 94,
    host: "canix402-api.compx.io",
    supported: true,
    provider: "CANIX402",
  },
  {
    id: "community:example-weather",
    url: "https://x402-weather.example.com/forecast",
    method: "GET",
    description: "Example community-listed weather forecast endpoint (mock data).",
    merchantId: "",
    network: "algorand-mainnet",
    testnet: false,
    amountMicros: 1000,
    asset: "31566704",
    // Reserved example.com host, and no invented address: this row exists
    // to give the community grid something to render in demo mode, not to
    // be a payable endpoint.
    payTo: "",
    params: [],
    settleCount: 12,
    host: "x402-weather.example.com",
    supported: false,
  },
];

export const bazaar = {
  list: async (opts: {
    offset: number;
    limit: number;
    q?: string;
    supported?: boolean;
  }): Promise<BazaarPage> => {
    if (BASE) {
      const qs = new URLSearchParams({
        offset: String(opts.offset),
        limit: String(opts.limit),
      });
      if (opts.q) qs.set("q", opts.q);
      if (opts.supported !== undefined) {
        qs.set("supported", opts.supported ? "1" : "0");
      }
      const res = await fetch(`${BASE}/bazaar/resources?${qs}`, {
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "could not load the catalog");
      return data as BazaarPage;
    }
    await delay(300);
    let items = MOCK_RESOURCES;
    if (opts.q) {
      const q = opts.q.toLowerCase();
      items = items.filter((r) =>
        `${r.url} ${r.description} ${r.provider ?? ""} ${r.host}`
          .toLowerCase()
          .includes(q),
      );
    }
    if (opts.supported !== undefined) {
      items = items.filter((r) => r.supported === opts.supported);
    }
    const supportedCount = MOCK_RESOURCES.filter((r) => r.supported).length;
    const page = items.slice(opts.offset, opts.offset + opts.limit);
    return {
      items: page,
      total: items.length,
      offset: opts.offset,
      limit: opts.limit,
      supportedCount,
    };
  },
};

// formatPrice renders an atomic amount (6 decimals) as a plain number, with
// no trailing zeros — 5000 -> "0.005". The unit isn't necessarily USDC; see
// assetSymbol.
export function formatPrice(amountMicros: number): string {
  return String(amountMicros / 1e6);
}

// assetSymbol mirrors backend/internal/engine/nodes/tool402.go's assetSymbol:
// a catalog entry's `asset` is an Algorand ASA id, not always USDC. Every
// other asset-aware display in the app (Inspector.tsx, canvas node chrome)
// falls back to "USDC" only because that's what a live Discover quote
// resolves to by default — this must not silently relabel a non-USDC
// endpoint's price as USDC before the node is even added.
const MAINNET_USDC_ASSET_ID = "31566704";
const TESTNET_USDC_ASSET_ID = "10458941";
export function assetSymbol(assetId: string): string {
  switch (assetId) {
    case "":
    case "0":
      return "ALGO";
    case MAINNET_USDC_ASSET_ID:
    case TESTNET_USDC_ASSET_ID:
      return "USDC";
    default:
      return `ASA-${assetId}`;
  }
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
  // (r.params ?? []): defense in depth. The backend guarantees a non-nil
  // array, but a mirror of an external catalog should never trust its own
  // past output blindly.
  const params = r.params ?? [];
  const paramDefaults: Record<string, string> = {};
  for (const p of params) paramDefaults[p.name] = "";
  const asset = assetSymbol(r.asset);

  return {
    type: "tool402",
    custom: true,
    icon: "✦",
    name: r.supported && r.provider ? r.provider : r.host,
    sub: `${formatPrice(r.amountMicros)} ${asset} / call`,
    endpoint: r.url,
    method: r.method,
    description: r.description,
    price: formatPrice(r.amountMicros),
    unit: "call",
    asset,
    provider: r.supported && r.provider ? r.provider : r.host,
    // Catalog data is a mirror, not a live quote. Leaving this false keeps the
    // Inspector honest: the price shown came from a cache, and Discover is
    // what confirms it against the endpoint's real 402 challenge.
    priceLive: false,
    discoveredParams: params.map((p) => ({
      name: p.name,
      type: p.type,
      required: p.required,
      description: p.description,
    })),
    paramDefaults,
  };
}

// A node handed from the Bazaar page to a workflow canvas travels in the URL,
// because the canvas only otherwise accepts nodes via a drag event that
// cannot cross a page boundary. base64url keeps it safe in a query string;
// encodeURIComponent first so non-Latin-1 characters survive btoa.
export function encodePendingNode(node: Partial<WorkflowNode>): string {
  const json = JSON.stringify(node);
  const b64 = btoa(encodeURIComponent(json));
  return b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// decodePendingNode is deliberately strict: this value comes from a URL a
// user (or a crafted link) can edit by hand, so it is whitelisted field by
// field to exactly the shape resourceToNode produces — never passed through
// as a parsed object — rather than dropped onto the canvas as a malformed or
// maliciously-extended node. https is required on the endpoint for the same
// reason keepResource on the backend never trusts a bare URL.
function isDiscoveredParam(p: unknown): p is {
  name: string;
  type: string;
  required: boolean;
  description: string;
} {
  if (!p || typeof p !== "object") return false;
  const o = p as Record<string, unknown>;
  return (
    typeof o.name === "string" &&
    typeof o.type === "string" &&
    typeof o.required === "boolean" &&
    typeof o.description === "string"
  );
}

export function decodePendingNode(raw: string): Partial<WorkflowNode> | null {
  try {
    const b64 = raw.replace(/-/g, "+").replace(/_/g, "/");
    const parsed = JSON.parse(decodeURIComponent(atob(b64)));
    if (!parsed || typeof parsed !== "object") return null;
    const p = parsed as Record<string, unknown>;
    if (p.type !== "tool402" || typeof p.endpoint !== "string") return null;
    if (!/^https:\/\//.test(p.endpoint)) return null;

    const node: Partial<WorkflowNode> = {
      type: "tool402",
      custom: true,
      icon: "✦",
      endpoint: p.endpoint,
    };
    if (typeof p.name === "string") node.name = p.name;
    if (typeof p.sub === "string") node.sub = p.sub;
    if (typeof p.method === "string") node.method = p.method;
    if (typeof p.description === "string") node.description = p.description;
    if (typeof p.price === "string") node.price = p.price;
    if (typeof p.unit === "string") node.unit = p.unit;
    if (typeof p.asset === "string") node.asset = p.asset;
    if (typeof p.provider === "string") node.provider = p.provider;
    if (typeof p.priceLive === "boolean") node.priceLive = p.priceLive;
    if (Array.isArray(p.discoveredParams)) {
      node.discoveredParams = p.discoveredParams.filter(isDiscoveredParam);
    }
    if (
      p.paramDefaults &&
      typeof p.paramDefaults === "object" &&
      !Array.isArray(p.paramDefaults)
    ) {
      const defaults: Record<string, string> = {};
      for (const [k, v] of Object.entries(
        p.paramDefaults as Record<string, unknown>,
      )) {
        if (typeof v === "string") defaults[k] = v;
      }
      node.paramDefaults = defaults;
    }
    return node;
  } catch {
    return null;
  }
}
