// TODO: Replace all stubs with real FastAPI calls when backend is ready.
// Base URL will come from env: process.env.NEXT_PUBLIC_API_URL

import {
  Workflow,
  UsageRange,
  UsageSummary,
  UsagePoint,
  WorkflowSpend,
  EndpointUsage,
  Settlement,
} from "./types";
import { WORKFLOWS, SAMPLE_WORKFLOW, buildUsage } from "./data";

// In the browser, always route through /api so the cookie stays same-site.
// NEXT_PUBLIC_API_URL still controls mock vs real (empty = mock data).
const _CONFIGURED = process.env.NEXT_PUBLIC_API_URL ?? "";
export const BASE =
  _CONFIGURED && typeof window !== "undefined" ? "/api" : _CONFIGURED;

// -- Auth ------------------------------------------------------------------
export interface AuthUser {
  id: string;
  email: string;
  name: string;
  orgName: string;
  // ISO 8601, from users.created_at. Shown as "member since" on the settings
  // page. Optional because a response cached from before the field existed
  // would otherwise type as present and render "Invalid Date".
  createdAt?: string;
  // Which currency the UI renders amounts in. Carried here rather than fetched
  // separately because every page already calls /auth/me, so a USD user gains
  // no extra request. Optional so a response cached from before the field
  // existed falls back to the default rather than reading as undefined.
  displayCurrency?: string;
  // True for an OAuth account that has never set a name/org — Google and
  // GitHub only hand back a verified email, not an organization.
  needsOnboarding: boolean;
}

// Mutable in mock mode so a profile edit persists for the session, the same
// way MOCK_SETTINGS does for the settings page.
const MOCK_PROFILE = { name: "Dev", orgName: "Acme Capital" };

export const auth = {
  signIn: async (email: string, password: string): Promise<void> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/signin`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "sign in failed");
      return;
    }
    void email;
    void password;
    await delay(400);
  },

  signUp: async (
    email: string,
    password: string,
    name: string,
    org: string,
  ): Promise<void> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/signup`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password, name, org }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "sign up failed");
      return;
    }
    void email;
    void password;
    void name;
    void org;
    await delay(500);
  },

  me: async (): Promise<AuthUser> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/me`, { credentials: "include" });
      if (!res.ok) throw new Error("unauthorized");
      return res.json();
    }
    return {
      id: "dev",
      email: "dev@local",
      // Read from the same mock object updateProfile writes, so a saved
      // name survives in mock mode instead of reverting to "Dev" on the
      // next read -- matching how MOCK_SETTINGS already behaves.
      name: MOCK_PROFILE.name,
      orgName: MOCK_PROFILE.orgName,
      createdAt: "2026-01-01T00:00:00Z",
      // Read from the same mock the settings endpoints mutate, so changing
      // currency in mock mode propagates exactly as it does in real mode
      // (where the server persists it and /auth/me reads it back).
      displayCurrency: MOCK_SETTINGS.displayCurrency,
      needsOnboarding: false,
    };
  },

  // Sets name/org for the signed-in user. Used by the post-OAuth onboarding
  // prompt (OAuth accounts start with no name/org), and reusable as a
  // general profile edit.
  updateProfile: async (name: string, orgName: string): Promise<AuthUser> => {
    if (BASE) {
      const res = await fetch(`${BASE}/auth/me`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, orgName }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "could not update profile");
      return data;
    }
    MOCK_PROFILE.name = name;
    MOCK_PROFILE.orgName = orgName;
    return {
      id: "dev",
      email: "dev@local",
      name,
      orgName,
      createdAt: "2026-01-01T00:00:00Z",
      displayCurrency: MOCK_SETTINGS.displayCurrency,
      needsOnboarding: false,
    };
  },

  // Changes the signed-in user's password after the server verifies the
  // current one. Throws with the server's own message so the form can show
  // "current password is incorrect" and the distinct OAuth-only case
  // ("this account signs in with Google or GitHub") without guessing which
  // happened from the status code alone.
  changePassword: async (
    currentPassword: string,
    newPassword: string,
  ): Promise<void> => {
    if (!BASE) throw new Error("changing a password requires a backend");
    const res = await fetch(`${BASE}/auth/password`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ currentPassword, newPassword }),
    });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error ?? "could not change password");
    }
  },

  signOut: async (): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/auth/signout`, {
        method: "POST",
        credentials: "include",
      });
      return;
    }
    await delay(100);
  },

  // Full URL to kick off a backend OAuth flow. Empty string when no backend
  // is configured (mock mode) -- callers should guard on the http prefix.
  oauthURL: (provider: "github" | "google"): string =>
    BASE ? `${BASE}/auth/oauth/${provider}` : "",
};

// -- Workflows ------------------------------------------------------------
export const workflows = {
  // TODO: GET /workflows
  list: async (): Promise<Workflow[]> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows`, { credentials: "include" });
      if (!res.ok) throw new Error("workflows fetch failed");
      return res.json();
    }
    await delay(200);
    return WORKFLOWS;
  },

  // TODO: GET /workflows/:id
  get: async (id: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, {
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "workflow fetch failed");
      return data;
    }
    await delay(150);
    if (id === "new")
      return { id: "wf-new", name: "Untitled workflow", nodes: [], edges: [] };
    return JSON.parse(JSON.stringify(SAMPLE_WORKFLOW));
  },

  // TODO: POST /workflows
  create: async (name: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "workflow create failed");
      return data;
    }
    await delay(300);
    return { id: `wf-${Date.now()}`, name, nodes: [], edges: [] };
  },

  // TODO: PUT /workflows/:id
  update: async (id: string, wf: Partial<Workflow>): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(wf),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "workflow update failed");
      return data;
    }
    await delay(200);
    return {
      id,
      name: wf.name ?? "Untitled",
      nodes: wf.nodes ?? [],
      edges: wf.edges ?? [],
    };
  },

  // DELETE /workflows/:id — permanent. The backend refuses (409) for a
  // workflow that has Tendril lease history, since deleting it would destroy
  // the only copy of an active lease's encrypted credentials; that message is
  // surfaced to the caller rather than swallowed.
  remove: async (id: string): Promise<void> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error ?? "workflow delete failed");
      }
      return;
    }
    await delay(200);
  },

  // TODO: POST /workflows/:id/deploy
  deploy: async (
    id: string,
  ): Promise<{
    agents: { nodeId: string; address: string; network: string }[];
  }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/deploy`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "deploy failed");
      return data;
    }
    await delay(800);
    return { agents: [] };
  },

  // TODO: POST /workflows/:id/run
  run: async (
    id: string,
    input?: Record<string, unknown>,
  ): Promise<{ runId: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/run`, {
        method: "POST",
        credentials: "include",
        headers: input ? { "Content-Type": "application/json" } : {},
        body: input ? JSON.stringify(input) : undefined,
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "run failed");
      return data;
    }
    await delay(200);
    return { runId: `r-${Math.floor(1800 + Math.random() * 200)}` };
  },

  // TODO: POST /workflows/:id/build
  build: async (
    id: string,
    message: string,
  ): Promise<{ reply: string; workflow: Workflow }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/build`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "build failed");
      return data;
    }
    await delay(300);
    // Echo the current mock workflow back untouched: the caller replaces its
    // nodes/edges with whatever comes back, so returning an empty graph here
    // would wipe the demo canvas on the first chat message.
    const current = await workflows.get(id);
    return {
      reply:
        "Mock build response — connect a real backend to build workflows from chat.",
      workflow: current,
    };
  },

  // TODO: POST /workflows/:id/stop
  stop: async (id: string): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/workflows/${id}/stop`, {
        method: "POST",
        credentials: "include",
      });
      return;
    }
    await delay(100);
  },
};

// -- Credits ----------------------------------------------------------------
export const credits = {
  // The authoritative balance: users.credit_balance_usd_micros, the same row
  // the engine reserves against and debits on every paid call. Anything shown
  // from another source is a guess that drifts the moment a run spends money.
  balance: async (): Promise<number> => {
    if (BASE) {
      const res = await fetch(`${BASE}/credits/balance`, {
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "balance fetch failed");
      return (data.credit_usd_micros ?? 0) / 1e6;
    }
    await delay(120);
    return 0;
  },

  // Redeems a coupon code, returning the new balance and what this code
  // granted — both in USD. The credited amount is per-code configuration
  // (COUPON_CODES on the backend), so it has to come from the response rather
  // than being assumed. Throws with the server's message (e.g. "invalid coupon
  // code", "coupon already redeemed") on failure so the caller can show it
  // directly.
  redeemCoupon: async (
    code: string,
  ): Promise<{ balanceUSD: number; creditedUSD: number }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/credits/redeem-coupon`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "coupon redemption failed");
      return {
        balanceUSD: (data.credit_usd_micros ?? 0) / 1e6,
        creditedUSD: (data.credited_usd_micros ?? 0) / 1e6,
      };
    }
    await delay(120);
    throw new Error("coupons aren't available in mock mode");
  },
};

// -- Settings ---------------------------------------------------------------
// Account-level preferences (user_settings). Amounts are USD micros, matching
// the ledgers — the UI converts for display and never stores a float.
export interface UserSettings {
  // Presentation only — never affects what is stored, charged, or settled.
  // "USD" means render exactly as the app did before this feature.
  displayCurrency: string;
  lowBalanceUsdMicros: number;
  // null / absent means no per-call ceiling of the user's own; the platform's
  // global cap still applies, so this is never "unlimited".
  maxCallSpendUsdMicros?: number | null;
}

// A PATCH sends only what changed. maxCallSpendUsdMicros is deliberately
// `number | null` rather than optional-undefined: the server treats an absent
// key as "leave it alone" and an explicit null as "remove my ceiling", and
// collapsing those two would silently drop a user's spend limit on every save.
export type UserSettingsPatch = Partial<{
  displayCurrency: string;
  lowBalanceUsdMicros: number;
  maxCallSpendUsdMicros: number | null;
}>;

const MOCK_SETTINGS: UserSettings = {
  displayCurrency: "USD",
  lowBalanceUsdMicros: 5_000_000,
  maxCallSpendUsdMicros: null,
};

export const settings = {
  // Never 404s: an account with no user_settings row gets the defaults.
  get: async (): Promise<UserSettings> => {
    if (BASE) {
      const res = await fetch(`${BASE}/settings`, { credentials: "include" });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "settings fetch failed");
      return data;
    }
    await delay(150);
    return { ...MOCK_SETTINGS };
  },

  // Returns the full merged settings, so callers replace their state with the
  // response rather than assuming the patch applied exactly as sent.
  update: async (patch: UserSettingsPatch): Promise<UserSettings> => {
    if (BASE) {
      const res = await fetch(`${BASE}/settings`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "could not save settings");
      return data;
    }
    await delay(200);
    Object.assign(MOCK_SETTINGS, patch);
    return { ...MOCK_SETTINGS };
  },
};

// -- Exchange rates ---------------------------------------------------------
// Display only. Top-ups never use these: the server fetches its own fresh rate
// at order time and locks it into the ledger row. Callers must only reach for
// this when the user's display currency is not USD.
export interface FXRateTable {
  base: string;
  rates: Record<string, number>;
  fetchedAt: string;
}

const MOCK_RATES: FXRateTable = {
  base: "USD",
  // Illustrative only, for mock mode. Real values come from the backend.
  rates: {
    USD: 1,
    INR: 95.25,
    EUR: 0.8655,
    GBP: 0.7415,
    JPY: 157.88,
    AUD: 1.4164,
    CAD: 1.3954,
    SGD: 1.2791,
    AED: 3.6725,
    CHF: 0.8085,
  },
  fetchedAt: "2026-08-10T00:02:31Z",
};

export const fx = {
  rates: async (): Promise<FXRateTable> => {
    if (BASE) {
      const res = await fetch(`${BASE}/fx/rates`, { credentials: "include" });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "exchange rate fetch failed");
      return data;
    }
    await delay(150);
    return { ...MOCK_RATES };
  },
};

// -- Runs -------------------------------------------------------------------
export interface RunLogRecord {
  id: string;
  runId: string;
  stepIndex: number;
  nodeId: string;
  nodeType: string;
  status: "pending" | "running" | "success" | "failed";
  output?: unknown;
  durationMs?: number;
  ts: string;
}

export const runs = {
  // The DB-backed source of truth for a run's logs — used as a reconciliation
  // fallback once the live SSE stream ends, since the stream's broker only
  // delivers events to clients subscribed at the exact moment they're
  // published (see sse/broker.go's non-blocking, unbuffered-per-subscriber
  // Publish): any run that finishes a step before/without a live subscriber
  // silently drops that step's event, with no replay. Polling this after
  // "done" (or a stream error) guarantees the console reflects what actually
  // happened server-side, not just whatever fraction of events the stream
  // happened to deliver live.
  get: async (
    runId: string,
  ): Promise<{ run: { status: string }; logs: RunLogRecord[] }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/runs/${runId}`, {
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "failed to fetch run");
      return data;
    }
    await delay(150);
    // Mock mode returns a realistic finished run rather than an empty one, so
    // the console and the chat panel can be exercised with no backend
    // attached: an agent answer to render as prose, and a paid tool402 step
    // so the activity strip has a real tool count and settled amount. Mirrors
    // SAMPLE_WORKFLOW's node ids and its $0.065/call x402 weather endpoint.
    const now = Date.now();
    const iso = (msAgo: number) => new Date(now - msAgo).toISOString();
    // One id, referenced everywhere it appears. Spelling it out per-field let
    // the receipt, the explorer link and the payment list drift apart.
    const mockTxId =
      "7F2AC9D1E4B8A6350C1D9E2F4A7B8C3D5E6F1A2B3C4D5E6F7A8B9C0D1E2F3A4B";
    return {
      run: { status: "success" },
      logs: [
        {
          id: "rl-1",
          runId,
          stepIndex: 0,
          nodeId: "n4",
          nodeType: "tool402",
          status: "success",
          output: {
            txId: mockTxId,
            amount: "0.065",
            settledUsdMicros: 65000,
            nodeName: "x402 Weather",
            explorerURL: `https://allo.info/tx/${mockTxId}`,
            response: {
              location: "San Francisco, CA",
              tempC: 14.2,
              condition: "Partly cloudy",
              windKph: 18,
            },
          },
          durationMs: 1900,
          ts: iso(6300),
        },
        {
          id: "rl-2",
          runId,
          stepIndex: 1,
          nodeId: "n2",
          nodeType: "agent",
          status: "success",
          output: {
            message:
              "It's 14.2°C in San Francisco right now and partly cloudy, with " +
              "winds around 18 km/h. Mild, but the wind makes it feel cooler — " +
              "worth a light jacket if you're heading out.",
            x402Payments: [{ txId: mockTxId, amount: "0.065" }],
          },
          durationMs: 4400,
          ts: iso(1900),
        },
      ],
    };
  },
};

// -- Agents ---------------------------------------------------------------
export const agents = {
  // TODO: GET /workflows/:wfId/agents/:agentId/balance
  balance: async (
    wfId: string,
    agentId: string,
  ): Promise<{ address: string; balance: string; network: string }> => {
    if (BASE) {
      const res = await fetch(
        `${BASE}/workflows/${wfId}/agents/${agentId}/balance`,
        { credentials: "include" },
      );
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "balance fetch failed");
      return data;
    }
    await delay(300);
    return { address: "", balance: "0.000000", network: "testnet" };
  },

  // TODO: POST /workflows/:wfId/agents/:agentId/fund
  fund: async (
    wfId: string,
    agentId: string,
    amount: number,
  ): Promise<{ txHash: string; balance: string }> => {
    if (BASE) {
      const res = await fetch(
        `${BASE}/workflows/${wfId}/agents/${agentId}/fund`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ amount }),
        },
      );
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "fund failed");
      return data;
    }
    await delay(500);
    return {
      txHash: `0x${Math.random().toString(16).slice(2, 10)}`,
      balance: amount.toFixed(3),
    };
  },
};

// -- Tools ----------------------------------------------------------------
export const tools = {
  x402quote: async (
    url: string,
  ): Promise<{
    price?: string;
    unit?: string;
    asset?: string;
    network?: string;
    recipient?: string;
    raw?: string;
    description?: string;
    // The HTTP method the target declares for itself, and whether its params
    // ride in the query string or the body — both read out of the endpoint's
    // own Bazaar extension, so the canvas configures an arbitrary endpoint
    // correctly without anyone hardcoding support for it.
    method?: string;
    paramsIn?: "query" | "body";
    params?: Array<{
      name: string;
      type: string;
      required: boolean;
      description: string;
      default?: string;
    }>;
  }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/tools/x402/quote`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "quote failed");
      return data;
    }
    await delay(600);
    return {
      price: "0.002",
      unit: "call",
      network: "algorand-testnet",
      recipient: "",
    };
  },
};

// -- OAuth2 connected accounts (Gmail/Sheets/Calendar/Drive) --------------
// Distinct from `auth` above: that signs a person INTO AgentMesh; this
// connects an EXTERNAL account a Google-type workflow node calls on the
// user's behalf. See backend/internal/api/handlers/oauth2creds.go.
export interface OAuthCredentialSummary {
  id: string;
  provider: string;
  accountLabel: string;
  scopes: string;
  expiresAt: string;
  createdAt: string;
}

export const oauth2 = {
  // A full-page redirect (Google's consent screen), not a fetch -- the
  // caller should set window.location.href to this, not call it as an
  // async request.
  connectURL: (provider: string): string => `${BASE}/oauth2/${provider}/start`,

  // Omitting the provider lists every connected account, which is what the
  // settings page shows; the canvas passes one to narrow to its own node.
  listCredentials: async (
    provider?: string,
  ): Promise<OAuthCredentialSummary[]> => {
    if (!BASE) return []; // No connected-account concept in mock mode.
    const qs = provider ? `?provider=${encodeURIComponent(provider)}` : "";
    const res = await fetch(`${BASE}/oauth2/credentials${qs}`, {
      credentials: "include",
    });
    if (!res.ok) throw new Error("could not load connected accounts");
    return res.json().catch(() => []);
  },

  deleteCredential: async (id: string): Promise<void> => {
    if (!BASE) return;
    const res = await fetch(
      `${BASE}/oauth2/credentials/${encodeURIComponent(id)}`,
      { method: "DELETE", credentials: "include" },
    );
    // Revoking is destructive and irreversible, so a failure has to reach
    // the caller. Swallowing it would report "disconnected" while the
    // credential is still live and still usable.
    if (!res.ok) throw new Error("could not disconnect this account");
  },
};

// -- Waitlist -------------------------------------------------------------
export const waitlist = {
  // TODO: POST /waitlist
  join: async (email: string): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/waitlist`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      return;
    }
    void email;
    await delay(600);
  },
};

// -- Payments ---------------------------------------------------------------
export const payments = {
  createCashfreeOrder: async (
    amountINRPaise: number,
    phone: string,
  ): Promise<{
    order_id: string;
    payment_session_id: string;
    amount: number;
    currency: string;
    app_id: string;
  }> => {
    if (!BASE) throw new Error("payments require a configured backend");
    const res = await fetch(`${BASE}/payments/cashfree/order`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ amount_inr_paise: amountINRPaise, phone }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error ?? "order creation failed");
    return data;
  },

  verifyCashfreePayment: async (
    orderId: string,
  ): Promise<{ status: string; credited_usd_micros: number }> => {
    if (!BASE) throw new Error("payments require a configured backend");
    const res = await fetch(`${BASE}/payments/cashfree/verify`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ order_id: orderId }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error ?? "payment verification failed");
    return data;
  },
};

// -- Usage & Credits ------------------------------------------------------
// Real endpoints don't exist yet (see plan §5 -- needs a metering change in
// tool402.go + provider.go). Until then these return fixtures in mock mode,
// and in real mode call the proposed /usage/* routes once the backend adds them.
// Mock fixtures depend on Date.now(); memoize per range so every panel in a
// render shares one consistent payload instead of regenerating timestamps.
const _usageCache = new Map<UsageRange, ReturnType<typeof buildUsage>>();
function mockUsage(range: UsageRange): ReturnType<typeof buildUsage> {
  let u = _usageCache.get(range);
  if (!u) {
    u = buildUsage(range);
    _usageCache.set(range, u);
  }
  return u;
}

// Bucket granularity has to track the range the chart actually plots: 24h is
// charted as 24 hourly points, 7d/30d as daily ones. Hardcoding bucket=day
// would collapse 24h to a single point once the backend honours the param.
function bucketFor(range: UsageRange): "hour" | "day" {
  return range === "24h" ? "hour" : "day";
}

// One fetch/mock branch for every usage endpoint. Always reads the response
// body for a server-provided `error` message -- before this was shared, only
// summary did, and the other four threw fixed strings that discarded detail.
async function usageFetch<T>(path: string, mock: () => T): Promise<T> {
  if (BASE) {
    const res = await fetch(`${BASE}${path}`, { credentials: "include" });
    const data = await res.json().catch(() => ({}));
    if (!res.ok)
      throw new Error(
        (data as { error?: string }).error ?? `usage request failed: ${path}`,
      );
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
  invalidate: (): void => {
    _usageCache.clear();
  },

  summary: (range: UsageRange): Promise<UsageSummary> =>
    usageFetch(`/usage/summary?range=${range}`, () => mockUsage(range).summary),

  timeseries: (range: UsageRange): Promise<UsagePoint[]> =>
    usageFetch(
      `/usage/timeseries?range=${range}&bucket=${bucketFor(range)}`,
      () => mockUsage(range).timeseries,
    ),

  byWorkflow: (range: UsageRange): Promise<WorkflowSpend[]> =>
    usageFetch(
      `/usage/by-workflow?range=${range}`,
      () => mockUsage(range).byWorkflow,
    ),

  byEndpoint: (range: UsageRange): Promise<EndpointUsage[]> =>
    usageFetch(
      `/usage/by-endpoint?range=${range}`,
      () => mockUsage(range).byEndpoint,
    ),

  // Settlements are the latest on-chain payments, not a range-scoped metric --
  // the real endpoint takes only `limit`, and the panel deliberately ignores
  // the 24h/7d/30d selector. Any range yields the same rows in mock mode, so
  // "30d" just picks a canonical memoized payload to slice from.
  settlements: (limit = 20): Promise<Settlement[]> =>
    usageFetch(`/usage/settlements?limit=${limit}`, () =>
      mockUsage("30d").settlements.slice(0, limit),
    ),
};

// -- Helpers --------------------------------------------------------------
function delay(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}
