import { describe, it, expect, vi, beforeEach } from "vitest";

// The deep link is the one input this module cannot control: it arrives from
// Android, after a round trip through a browser and a provider, and every way
// it can be wrong ends with the user looking at a sign-in screen. So these
// tests are about what handleCallbackUrl decides, not about the bridge.

const VERIFIER_KEY = "agentmesh.oauth.verifier";

type ExchangeCall = { code: string; verifier: string };

// Only the four members this module uses.
function fakeSessionStorage(initial: Record<string, string> = {}) {
  const store = { ...initial };
  return {
    store,
    api: {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => {
        store[k] = v;
      },
      removeItem: (k: string) => {
        delete store[k];
      },
      clear: () => {
        for (const k of Object.keys(store)) delete store[k];
      },
    },
  };
}

async function loadModule(opts: {
  session?: Record<string, string>;
  exchange?: (code: string, verifier: string) => Promise<string | null>;
  calls?: ExchangeCall[];
}) {
  const session = fakeSessionStorage(opts.session);
  vi.doMock("./secureStore", () => ({ SecureStore: {
    get: async ({ key }: { key: string }) => ({ value: session.api.getItem(key) }),
    set: async ({ key, value }: { key: string; value: string }) => session.api.setItem(key, value),
    remove: async ({ key }: { key: string }) => session.api.removeItem(key),
  } }));

  vi.doMock("@/lib/api", () => ({
    auth: {
      nativeOAuthURL: async (p: string) => `https://app.test/api/auth/oauth/${p}`,
      oauthExchange: async (code: string, verifier: string) => {
        opts.calls?.push({ code, verifier });
        return opts.exchange
          ? await opts.exchange(code, verifier)
          : "session-token";
      },
    },
  }));

  const mod = await import("./oauth");
  return { mod, session };
}

beforeEach(() => {
  vi.resetModules();
  vi.unstubAllGlobals();
});

describe("handleCallbackUrl", () => {
  it("exchanges the code for a token and clears the verifier", async () => {
    const calls: ExchangeCall[] = [];
    const { mod, session } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
      calls,
    });

    const result = await mod.handleCallbackUrl(
      "ai.agentmesh.app://auth?code=one-time",
    );

    expect(result).toEqual({ ok: true, token: "session-token" });
    expect(calls).toEqual([{ code: "one-time", verifier: "the-verifier" }]);
    // Cleared even on success: a verifier left behind would be reused by the
    // next attempt, and the backend would refuse the pair.
    expect(session.store[VERIFIER_KEY]).toBeUndefined();
  });

  it("reports the backend's reason when the callback carries an error", async () => {
    const calls: ExchangeCall[] = [];
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
      calls,
    });

    const result = await mod.handleCallbackUrl(
      "ai.agentmesh.app://auth?error=account_exists",
    );

    expect(result).toEqual({ ok: false, reason: "account_exists" });
    // Nothing to exchange, so nothing should have been sent.
    expect(calls).toEqual([]);
  });

  it("fails when there is no pending sign-in", async () => {
    const { mod } = await loadModule({ session: {} });

    const result = await mod.handleCallbackUrl(
      "ai.agentmesh.app://auth?code=one-time",
    );

    expect(result).toEqual({ ok: false, reason: "no_verifier" });
  });

  it("fails when the backend refuses the code", async () => {
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
      exchange: async () => null,
    });

    const result = await mod.handleCallbackUrl(
      "ai.agentmesh.app://auth?code=expired",
    );

    expect(result).toEqual({ ok: false, reason: "exchange_failed" });
  });

  it("fails rather than throws when the exchange call itself blows up", async () => {
    // This runs inside a listener with nobody awaiting it, so a rejection here
    // would be an unhandled one and the user would see nothing at all.
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
      exchange: async () => {
        throw new Error("offline");
      },
    });

    const result = await mod.handleCallbackUrl(
      "ai.agentmesh.app://auth?code=one-time",
    );

    expect(result).toEqual({ ok: false, reason: "exchange_failed" });
  });

  it("rejects a callback with no code", async () => {
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
    });

    expect(await mod.handleCallbackUrl("ai.agentmesh.app://auth")).toEqual({
      ok: false,
      reason: "no_code",
    });
  });

  it("rejects something that is not a URL at all", async () => {
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
    });

    expect(await mod.handleCallbackUrl("not a url")).toEqual({
      ok: false,
      reason: "bad_callback",
    });
  });
});

describe("listenForCallback", () => {
  // A second attachment would run one callback twice, and the second run finds
  // the verifier already cleared -- so it would report a failure over a sign-in
  // that actually worked.
  it("attaches once however many times it is called", async () => {
    const attached: string[] = [];
    vi.doMock("@capacitor/app", () => ({
      App: {
        getLaunchUrl: async () => undefined,
        addListener: async (event: string) => {
          attached.push(event);
          return { remove: async () => {} };
        },
      },
    }));

    const { mod } = await loadModule({ session: {} });
    await mod.listenForCallback(() => {});
    await mod.listenForCallback(() => {});

    expect(attached).toEqual(["appUrlOpen"]);
  });

  it("ignores a deep link that is not the auth callback", async () => {
    // singleTask means every VIEW intent for this app arrives here, not only
    // ours. Acting on someone else's would clear the verifier mid-flow.
    let fire: ((e: { url: string }) => void) | undefined;
    vi.doMock("@capacitor/app", () => ({
      App: {
        getLaunchUrl: async () => undefined,
        addListener: async (_e: string, cb: (e: { url: string }) => void) => {
          fire = cb;
          return { remove: async () => {} };
        },
      },
    }));

    const seen: unknown[] = [];
    const { mod } = await loadModule({
      session: { [VERIFIER_KEY]: "the-verifier" },
    });
    await mod.listenForCallback((r) => {
      seen.push(r);
    });

    fire?.({ url: "ai.agentmesh.app://something-else?code=x" });
    await new Promise((r) => setTimeout(r, 0));

    expect(seen).toEqual([]);
  });
});
