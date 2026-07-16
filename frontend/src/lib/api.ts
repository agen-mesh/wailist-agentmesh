// TODO: Replace all stubs with real FastAPI calls when backend is ready.
// Base URL will come from env: process.env.NEXT_PUBLIC_API_URL

import {
  Workflow,
  UsageRange, UsageSummary, UsagePoint, WorkflowSpend, EndpointUsage, Settlement,
} from "./types";
import { WORKFLOWS, SAMPLE_WORKFLOW, buildUsage } from "./data";

// In the browser, always route through /api so the cookie stays same-site.
// NEXT_PUBLIC_API_URL still controls mock vs real (empty = mock data).
const _CONFIGURED = process.env.NEXT_PUBLIC_API_URL ?? "";
const BASE = _CONFIGURED && typeof window !== "undefined" ? "/api" : _CONFIGURED;

// -- Auth ------------------------------------------------------------------
export const auth = {
  signIn: async (email: string, password: string): Promise<void> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/signin`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "sign in failed");
      return;
    }
    void email; void password;
    await delay(400);
  },

  signUp: async (email: string, password: string, org: string): Promise<void> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/signup`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password, org }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "sign up failed");
      return;
    }
    void email; void password; void org;
    await delay(500);
  },

  me: async (): Promise<{ id: string; email: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/me`, { credentials: "include" });
      if (!res.ok) throw new Error("unauthorized");
      return res.json();
    }
    return { id: "dev", email: "dev@local" };
  },

  signOut: async (): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/auth/signout`, { method: "POST", credentials: "include" });
      return;
    }
    await delay(100);
  },

  // Full URL to kick off a backend OAuth flow. Empty string when no backend
  // is configured (mock mode) — callers should guard on the http prefix.
  oauthURL: (provider: "github" | "google"): string =>
    BASE ? `${BASE}/auth/oauth/${provider}` : "",
};

// -- Workflows ------------------------------------------------------------
export const workflows = {
  // TODO: GET /workflows
  list: async (): Promise<Workflow[]> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows`, { credentials: "include" });
      return res.json();
    }
    await delay(200);
    return WORKFLOWS;
  },

  // TODO: GET /workflows/:id
  get: async (id: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, { credentials: "include" });
      return res.json();
    }
    await delay(150);
    if (id === "new") return { id: "wf-new", name: "Untitled workflow", nodes: [], edges: [] };
    return JSON.parse(JSON.stringify(SAMPLE_WORKFLOW));
  },

  // TODO: POST /workflows
  create: async (name: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      });
      return res.json();
    }
    await delay(300);
    return { id: `wf-${Date.now()}`, name, nodes: [], edges: [] };
  },

  // TODO: PUT /workflows/:id
  update: async (id: string, wf: Partial<Workflow>): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, {
        method: "PUT", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(wf),
      });
      return res.json();
    }
    await delay(200);
    return { id, name: wf.name ?? "Untitled", nodes: wf.nodes ?? [], edges: wf.edges ?? [] };
  },

  // TODO: POST /workflows/:id/deploy
  deploy: async (id: string): Promise<{ agents: { nodeId: string; address: string; network: string }[] }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/deploy`, {
        method: "POST", credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "deploy failed");
      return data;
    }
    await delay(800);
    return { agents: [] };
  },

  // TODO: POST /workflows/:id/run
  run: async (id: string, input?: Record<string, unknown>): Promise<{ runId: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/run`, {
        method: "POST", credentials: "include",
        headers: input ? { "Content-Type": "application/json" } : {},
        body: input ? JSON.stringify(input) : undefined,
      });
      return res.json();
    }
    await delay(200);
    return { runId: `r-${Math.floor(1800 + Math.random() * 200)}` };
  },

  // TODO: POST /workflows/:id/stop
  stop: async (id: string): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/workflows/${id}/stop`, { method: "POST", credentials: "include" });
      return;
    }
    await delay(100);
  },
};

// -- Agents ---------------------------------------------------------------
export const agents = {
  // TODO: GET /workflows/:wfId/agents/:agentId/balance
  balance: async (wfId: string, agentId: string): Promise<{ address: string; balance: string; network: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${wfId}/agents/${agentId}/balance`, { credentials: "include" });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "balance fetch failed");
      return data;
    }
    await delay(300);
    return { address: "", balance: "0.000000", network: "testnet" };
  },

  // TODO: POST /workflows/:wfId/agents/:agentId/fund
  fund: async (wfId: string, agentId: string, amount: number): Promise<{ txHash: string; balance: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${wfId}/agents/${agentId}/fund`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ amount }),
      });
      return res.json();
    }
    await delay(500);
    return { txHash: `0x${Math.random().toString(16).slice(2, 10)}`, balance: amount.toFixed(3) };
  },
};

// -- Tools ----------------------------------------------------------------
export const tools = {
  x402quote: async (url: string): Promise<{
    price?: string; unit?: string; network?: string; recipient?: string; raw?: string; description?: string;
    params?: Array<{ name: string; type: string; required: boolean; description: string; default?: string }>;
  }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/tools/x402/quote`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "quote failed");
      return data;
    }
    await delay(600);
    return { price: "0.002", unit: "call", network: "algorand-testnet", recipient: "" };
  },
};

// -- Waitlist -------------------------------------------------------------
export const waitlist = {
  // TODO: POST /waitlist
  join: async (email: string): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/waitlist`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      return;
    }
    void email;
    await delay(600);
  },
};

// -- Usage & Credits ------------------------------------------------------
// Real endpoints don't exist yet (see plan §5 — needs a metering change in
// tool402.go + provider.go). Until then these return fixtures in mock mode,
// and in real mode call the proposed /usage/* routes once the backend adds them.
// Mock fixtures depend on Date.now(); memoize per range so every panel in a
// render shares one consistent payload instead of regenerating timestamps.
const _usageCache = new Map<UsageRange, ReturnType<typeof buildUsage>>();
function mockUsage(range: UsageRange): ReturnType<typeof buildUsage> {
  let u = _usageCache.get(range);
  if (!u) { u = buildUsage(range); _usageCache.set(range, u); }
  return u;
}

// One fetch/mock branch for every usage endpoint. Always reads the response
// body for a server-provided `error` message — before this was shared, only
// summary did, and the other four threw fixed strings that discarded detail.
async function usageFetch<T>(path: string, mock: () => T): Promise<T> {
  if (BASE) {
    const res = await fetch(`${BASE}${path}`, { credentials: "include" });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as { error?: string }).error ?? `usage request failed: ${path}`);
    return data as T;
  }
  await delay(220);
  return mock();
}

export const usage = {
  // Drops the memoized mock payloads so the next fetch regenerates them.
  // Called by the retry action: without this, retry re-resolves from the cache
  // and looks like a no-op in mock mode. Harmless in real mode (the cache is
  // only read for fixtures).
  invalidate: (): void => { _usageCache.clear(); },

  summary: (range: UsageRange): Promise<UsageSummary> =>
    usageFetch(`/usage/summary?range=${range}`, () => mockUsage(range).summary),

  timeseries: (range: UsageRange): Promise<UsagePoint[]> =>
    usageFetch(`/usage/timeseries?range=${range}&bucket=day`, () => mockUsage(range).timeseries),

  byWorkflow: (range: UsageRange): Promise<WorkflowSpend[]> =>
    usageFetch(`/usage/by-workflow?range=${range}`, () => mockUsage(range).byWorkflow),

  byEndpoint: (range: UsageRange): Promise<EndpointUsage[]> =>
    usageFetch(`/usage/by-endpoint?range=${range}`, () => mockUsage(range).byEndpoint),

  // Settlements are the latest on-chain payments, not a range-scoped metric —
  // the real endpoint takes only `limit`, and the panel deliberately ignores
  // the 24h/7d/30d selector. Any range yields the same rows in mock mode, so
  // "30d" just picks a canonical memoized payload to slice from.
  settlements: (limit = 20): Promise<Settlement[]> =>
    usageFetch(`/usage/settlements?limit=${limit}`, () => mockUsage("30d").settlements.slice(0, limit)),
};

// -- Helpers --------------------------------------------------------------
function delay(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}
