// TODO: Replace all stubs with real FastAPI calls when backend is ready.
// Base URL will come from env: process.env.NEXT_PUBLIC_API_URL

import { Workflow } from "./types";
import { WORKFLOWS, SAMPLE_WORKFLOW } from "./data";

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

// -- Auth ------------------------------------------------------------------
export const auth = {
  // TODO: POST /auth/signin
  signIn: async (email: string, _password: string): Promise<{ token: string }> => {
    void email;
    await delay(400);
    return { token: "mock-jwt-token" };
  },

  // TODO: POST /auth/signup
  signUp: async (email: string, _password: string, _org: string): Promise<{ token: string }> => {
    void email;
    await delay(500);
    return { token: "mock-jwt-token" };
  },

  // TODO: POST /auth/signout
  signOut: async (): Promise<void> => {
    await delay(100);
  },
};

// -- Workflows ------------------------------------------------------------
export const workflows = {
  // TODO: GET /workflows
  list: async (): Promise<Workflow[]> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows`, { headers: authHeaders() });
      return res.json();
    }
    await delay(200);
    return WORKFLOWS;
  },

  // TODO: GET /workflows/:id
  get: async (id: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}`, { headers: authHeaders() });
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
        method: "POST", headers: { ...authHeaders(), "Content-Type": "application/json" },
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
        method: "PUT", headers: { ...authHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify(wf),
      });
      return res.json();
    }
    await delay(200);
    return { id, name: wf.name ?? "Untitled", nodes: wf.nodes ?? [], edges: wf.edges ?? [] };
  },

  // TODO: POST /workflows/:id/deploy
  deploy: async (id: string): Promise<{ agentWallets: Record<string, string> }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/deploy`, {
        method: "POST", headers: authHeaders(),
      });
      return res.json();
    }
    await delay(800);
    return { agentWallets: {} };
  },

  // TODO: POST /workflows/:id/run
  run: async (id: string): Promise<{ runId: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${id}/run`, {
        method: "POST", headers: authHeaders(),
      });
      return res.json();
    }
    await delay(200);
    return { runId: `r-${Math.floor(1800 + Math.random() * 200)}` };
  },

  // TODO: POST /workflows/:id/stop
  stop: async (id: string): Promise<void> => {
    if (BASE) {
      await fetch(`${BASE}/workflows/${id}/stop`, { method: "POST", headers: authHeaders() });
      return;
    }
    await delay(100);
  },
};

// -- Agents ---------------------------------------------------------------
export const agents = {
  // TODO: POST /workflows/:wfId/agents/:agentId/fund
  fund: async (wfId: string, agentId: string, amount: number): Promise<{ txHash: string; balance: string }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/workflows/${wfId}/agents/${agentId}/fund`, {
        method: "POST", headers: { ...authHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify({ amount }),
      });
      return res.json();
    }
    await delay(500);
    return { txHash: `0x${Math.random().toString(16).slice(2, 10)}`, balance: amount.toFixed(3) };
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

// -- Helpers --------------------------------------------------------------
function delay(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

function authHeaders(): Record<string, string> {
  if (typeof window === "undefined") return {};
  const token = localStorage.getItem("agentmesh_token");
  return token ? { Authorization: `Bearer ${token}` } : {};
}
