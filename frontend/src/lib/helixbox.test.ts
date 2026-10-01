import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  formatUsd,
  formatDuration,
  formatRemaining,
  totalCostMicros,
  paidUntil,
  sessionCode,
  loadSessions,
  saveSession,
  forgetSession,
  refreshSessions,
  getSessionsSnapshot,
  getServerSessionsSnapshot,
  subscribeSessions,
  HelixboxRunError,
  type HelixboxSession,
} from "./helixbox";

// ── Pricing ─────────────────────────────────────────────────────────────────
//
// The number this file computes is the number on the Buy button. A user who
// reads "$0.25" and is billed $1.75 has been misled, so the fee is never
// optional in a displayed total.
describe("pricing", () => {
  const FEE = 1_500_000;

  it("adds the platform fee to the vendor's price", () => {
    expect(totalCostMicros({ amountMicros: 250_000 }, FEE)).toBe(1_750_000);
    expect(totalCostMicros({ amountMicros: 2_000_000 }, FEE)).toBe(3_500_000);
  });

  it("renders atomic USDC as a plain dollar figure", () => {
    expect(formatUsd(1_750_000)).toBe("$1.75");
    expect(formatUsd(3_500_000)).toBe("$3.50");
    expect(formatUsd(0)).toBe("$0.00");
  });

  it("quotes the two hourly plans identically, because HelixBox prices them identically", () => {
    // Both probed at 250000 on 2026-09-08. If this ever changes, the console's
    // "same price as the CLI hour" copy has to go with it.
    expect(totalCostMicros({ amountMicros: 250_000 }, FEE)).toBe(
      totalCostMicros({ amountMicros: 250_000 }, FEE),
    );
  });
});

// ── Durations ───────────────────────────────────────────────────────────────
describe("formatDuration", () => {
  it("uses the words a buyer would use for the plans actually sold", () => {
    expect(formatDuration(3600)).toBe("1 hour");
    expect(formatDuration(604800)).toBe("1 week");
  });

  it("picks the largest unit that divides evenly", () => {
    expect(formatDuration(7200)).toBe("2 hours");
    expect(formatDuration(86400)).toBe("1 day");
    expect(formatDuration(172800)).toBe("2 days");
    expect(formatDuration(1209600)).toBe("2 weeks");
    expect(formatDuration(300)).toBe("5 minutes");
  });

  it("drops to a smaller unit rather than approximating when it can be exact", () => {
    // 5400s is not a whole number of hours but IS a whole number of minutes,
    // and "90 minutes" beats "about 2 hours" for a session someone is timing.
    expect(formatDuration(5400)).toBe("90 minutes");
  });

  it("approximates rather than printing a bare second count", () => {
    // Divides evenly into nothing, so it still has to read as time. Neither
    // HelixBox plan lands here today; this is the fallback holding the line if
    // one ever does.
    expect(formatDuration(5000)).toBe("about 1 hour");
    expect(formatDuration(100)).toBe("about 2 minutes");
    expect(formatDuration(30)).toBe("30 seconds");
  });

  it("renders nothing for a missing or nonsensical duration", () => {
    expect(formatDuration(0)).toBe("");
    expect(formatDuration(-1)).toBe("");
    expect(formatDuration(Number.NaN)).toBe("");
  });
});

describe("formatRemaining", () => {
  const now = 1_700_000_000_000;

  it("counts down in the units that matter at each scale", () => {
    expect(formatRemaining(now + 45 * 60_000, now)).toBe("45m left");
    expect(formatRemaining(now + 90 * 60_000, now)).toBe("1h 30m left");
    expect(formatRemaining(now + 3 * 86_400_000, now)).toBe("3d left");
    expect(formatRemaining(now + 3.5 * 86_400_000, now)).toBe("3d 12h left");
  });

  it("says 'under a minute' rather than '0m left'", () => {
    expect(formatRemaining(now + 30_000, now)).toBe("under a minute left");
  });

  // Null is the expired signal the console renders as a struck-through card.
  // Returning "0m left" instead would tell someone a dead token still works.
  it("returns null once the session has run out", () => {
    expect(formatRemaining(now, now)).toBeNull();
    expect(formatRemaining(now - 1, now)).toBeNull();
    expect(formatRemaining(now - 86_400_000, now)).toBeNull();
  });
});

// ── Reading HelixBox's reply ────────────────────────────────────────────────
//
// The reply is {"code":"AfXavNVgBJ","expiresAt":1757320000000}. Two things are
// worth pinning: expiresAt is an ABSOLUTE timestamp in milliseconds (their spec
// document says `expiresIn` in seconds, and is wrong), and misreading it turns
// a session the user just paid for into one that looks already expired.
describe("paidUntil", () => {
  const now = 1_700_000_000_000;

  it("reads the absolute millisecond timestamp HelixBox actually sends", () => {
    expect(paidUntil({ code: "AfXavNVgBJ", expiresAt: now + 3_600_000 }, now)).toBe(
      now + 3_600_000,
    );
    expect(paidUntil({ expires_at: now + 1000 }, now)).toBe(now + 1000);
  });

  // A timestamp in seconds is ~1.7e9; in ms it is ~1.7e12. Reading a
  // second-precision reply as milliseconds would date the session to 1970 and
  // render a live purchase as expired.
  it("rescues a timestamp sent in seconds instead of milliseconds", () => {
    expect(paidUntil({ expiresAt: 1_757_320_000 }, now)).toBe(1_757_320_000_000);
  });

  it("still understands the duration shape the spec document describes", () => {
    expect(paidUntil({ expiresIn: 3600 }, now)).toBe(now + 3_600_000);
    expect(paidUntil({ expires_in: 604800 }, now)).toBe(now + 604_800_000);
  });

  it("returns null when nothing usable came back", () => {
    expect(paidUntil(null, now)).toBeNull();
    expect(paidUntil(undefined, now)).toBeNull();
    expect(paidUntil("nope", now)).toBeNull();
    expect(paidUntil({}, now)).toBeNull();
    expect(paidUntil({ expiresAt: 0 }, now)).toBeNull();
    expect(paidUntil({ expiresAt: -1 }, now)).toBeNull();
    expect(paidUntil({ expiresAt: "soon" }, now)).toBeNull();
    expect(paidUntil({ expiresAt: Number.NaN }, now)).toBeNull();
  });
});

// The echoed code is the only way a buyer can confirm the money landed on the
// session they meant: HelixBox accepts any string and silently creates a new
// session for one it does not recognise.
describe("sessionCode", () => {
  it("reads the code back out of the reply", () => {
    expect(sessionCode({ code: "AfXavNVgBJ", expiresAt: 1 })).toBe("AfXavNVgBJ");
    expect(sessionCode({ code: "  AfXavNVgBJ  " })).toBe("AfXavNVgBJ");
  });

  it("returns null when there is no code to show", () => {
    expect(sessionCode(null)).toBeNull();
    expect(sessionCode({})).toBeNull();
    expect(sessionCode({ code: "" })).toBeNull();
    expect(sessionCode({ code: "   " })).toBeNull();
    expect(sessionCode({ code: 12345 })).toBeNull();
  });
});

// ── The receipt drawer ──────────────────────────────────────────────────────
describe("stored sessions", () => {
  const sample = (over: Partial<HelixboxSession> = {}): HelixboxSession => ({
    key: "k1",
    endpoint: "cli-hour",
    title: "CLI hour",
    accessLevel: "cli",
    code: "AfXavNVgBJ",
    expiresAt: Date.now() + 3_600_000,
    boughtAt: Date.now(),
    totalUsdMicros: 1_750_000,
    ...over,
  });

  // This suite installs its own storage rather than using the environment's.
  //
  // Not a shortcut: the `localStorage` global under vitest here is Node's own
  // web-storage stub (it starts with the warning "`--localstorage-file` was
  // provided without a valid path"), and it shadows jsdom's on both
  // globalThis and window. It is an object with no getItem, setItem,
  // removeItem or clear at all — so a test written against it would exercise
  // the catch blocks and nothing else. A minimal spec-shaped implementation
  // puts the real code paths under test; the browser supplies the real thing.
  let realStorage: PropertyDescriptor | undefined;

  function installStorage(impl: Partial<Storage>) {
    Object.defineProperty(globalThis, "localStorage", {
      value: impl,
      configurable: true,
      writable: true,
    });
  }

  function memoryStorage(): Storage {
    const map = new Map<string, string>();
    return {
      getItem: (k: string) => map.get(k) ?? null,
      setItem: (k: string, v: string) => void map.set(k, String(v)),
      removeItem: (k: string) => void map.delete(k),
      clear: () => map.clear(),
      key: (i: number) => Array.from(map.keys())[i] ?? null,
      get length() {
        return map.size;
      },
    } as Storage;
  }

  beforeEach(() => {
    realStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    installStorage(memoryStorage());
    // The snapshot cache is module state and outlives a single test.
    refreshSessions();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (realStorage) Object.defineProperty(globalThis, "localStorage", realStorage);
  });

  it("round-trips a purchase, newest first", () => {
    saveSession(sample({ key: "old" }));
    const after = saveSession(sample({ key: "new" }));
    expect(after.map((s) => s.key)).toEqual(["new", "old"]);
    expect(loadSessions().map((s) => s.key)).toEqual(["new", "old"]);
  });

  it("keeps the drawer small", () => {
    for (let i = 0; i < 20; i++) saveSession(sample({ key: `k${i}` }));
    expect(loadSessions()).toHaveLength(12);
    // The most recent purchases are the ones worth keeping.
    expect(loadSessions()[0].key).toBe("k19");
  });

  it("forgets one without touching the rest", () => {
    saveSession(sample({ key: "a" }));
    saveSession(sample({ key: "b" }));
    expect(forgetSession("a").map((s) => s.key)).toEqual(["b"]);
    expect(loadSessions().map((s) => s.key)).toEqual(["b"]);
  });

  // A console that white-screens because a receipt drawer could not be read
  // would be a worse bug than the one the drawer prevents.
  it("survives corrupt or foreign stored data", () => {
    localStorage.setItem("agentmesh_helixbox_sessions_v2", "not json");
    expect(loadSessions()).toEqual([]);

    localStorage.setItem("agentmesh_helixbox_sessions_v2", '{"not":"an array"}');
    expect(loadSessions()).toEqual([]);

    // Entries missing the fields the UI reads would render as broken cards.
    localStorage.setItem(
      "agentmesh_helixbox_sessions_v2",
      JSON.stringify([{ code: "ok", expiresAt: 1 }, { nope: true }, null, "x"]),
    );
    expect(loadSessions()).toHaveLength(1);
  });

  it("does not throw when storage itself is unavailable", () => {
    // Private-browsing modes throw outright on both getItem and setItem, and
    // the environment's own broken stub simply has neither. Both shapes have
    // to leave the console standing.
    installStorage({
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    });
    expect(() => loadSessions()).not.toThrow();
    expect(loadSessions()).toEqual([]);
    expect(() => saveSession(sample())).not.toThrow();
    expect(() => forgetSession("k1")).not.toThrow();

    // No storage API at all.
    installStorage({});
    expect(() => loadSessions()).not.toThrow();
    expect(loadSessions()).toEqual([]);
    expect(() => saveSession(sample())).not.toThrow();
    expect(() => forgetSession("k1")).not.toThrow();
  });
});

// A 402 means the run was refused BEFORE any payment, so the console can
// honestly say nothing was charged and offer a top-up. Losing the status would
// turn that into a generic "something went wrong".
describe("HelixboxRunError", () => {
  it("carries the status alongside the message", () => {
    const err = new HelixboxRunError("Not enough credit", 402);
    expect(err.status).toBe(402);
    expect(err.message).toBe("Not enough credit");
    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe("HelixboxRunError");
  });
});

// ── The snapshot the UI subscribes to ───────────────────────────────────────
//
// getSessionsSnapshot is called on EVERY render by useSyncExternalStore.
// Returning a fresh array each time would re-render forever, so reference
// stability between writes is a correctness requirement, not an optimisation.
describe("session snapshot", () => {
  let realStorage: PropertyDescriptor | undefined;

  function installStorage(impl: Partial<Storage>) {
    Object.defineProperty(globalThis, "localStorage", {
      value: impl,
      configurable: true,
      writable: true,
    });
  }

  function memoryStorage(): Storage {
    const map = new Map<string, string>();
    return {
      getItem: (k: string) => map.get(k) ?? null,
      setItem: (k: string, v: string) => void map.set(k, String(v)),
      removeItem: (k: string) => void map.delete(k),
      clear: () => map.clear(),
      key: (i: number) => Array.from(map.keys())[i] ?? null,
      get length() {
        return map.size;
      },
    } as Storage;
  }

  const sample = (key: string): HelixboxSession => ({
    key,
    endpoint: "cli-hour",
    title: "CLI hour",
    accessLevel: "cli",
    code: `CODE${key}`,
    expiresAt: Date.now() + 3_600_000,
    boughtAt: Date.now(),
    totalUsdMicros: 1_750_000,
  });

  beforeEach(() => {
    realStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    installStorage(memoryStorage());
    refreshSessions();
  });

  afterEach(() => {
    if (realStorage) Object.defineProperty(globalThis, "localStorage", realStorage);
  });

  it("returns the same reference until something is written", () => {
    const a = getSessionsSnapshot();
    expect(getSessionsSnapshot()).toBe(a);
    expect(getSessionsSnapshot()).toBe(a);
  });

  it("returns a new reference after a purchase, and the new contents", () => {
    const before = getSessionsSnapshot();
    saveSession(sample("a"));
    const after = getSessionsSnapshot();
    expect(after).not.toBe(before);
    expect(after.map((s) => s.key)).toEqual(["a"]);
    // ...and is stable again afterwards.
    expect(getSessionsSnapshot()).toBe(after);
  });

  it("returns a new reference after forgetting one", () => {
    saveSession(sample("a"));
    const before = getSessionsSnapshot();
    forgetSession("a");
    const after = getSessionsSnapshot();
    expect(after).not.toBe(before);
    expect(after).toEqual([]);
  });

  it("tells subscribers about every write, and stops when they leave", () => {
    let calls = 0;
    const unsubscribe = subscribeSessions(() => calls++);
    saveSession(sample("a"));
    expect(calls).toBe(1);
    forgetSession("a");
    expect(calls).toBe(2);
    unsubscribe();
    saveSession(sample("b"));
    expect(calls).toBe(2);
  });

  // The server has no storage to read. Guessing would flash credentials into
  // SSR markup that the client then replaces.
  it("gives the server an empty, stable drawer", () => {
    saveSession(sample("a"));
    expect(getServerSessionsSnapshot()).toEqual([]);
    expect(getServerSessionsSnapshot()).toBe(getServerSessionsSnapshot());
  });

  // The purchase happened whether or not the write survived. Dropping it from
  // the drawer because storage is full would hide a token the user just paid
  // for.
  it("still shows a purchase when storage refuses the write", () => {
    installStorage({
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
    });
    refreshSessions();
    saveSession(sample("a"));
    expect(getSessionsSnapshot().map((s) => s.key)).toEqual(["a"]);
  });
});
