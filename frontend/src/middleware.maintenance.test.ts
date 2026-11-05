import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest, type NextResponse } from "next/server";
import {
  getRewrittenUrl,
  isRewrite,
  unstable_doesMiddlewareMatch,
} from "next/experimental/testing/server";
import { config, middleware } from "./middleware";

const ORIGIN = "https://app.example.test";
const TOKEN = "s3cret-hackathon-token-0123456789";
const COOKIE = "agentmesh_maintenance_bypass";

function get(path: string, cookie?: string) {
  return middleware(
    new NextRequest(`${ORIGIN}${path}`, {
      headers: cookie ? { cookie } : undefined,
    }),
  );
}

function rewrittenPath(res: NextResponse): string | null {
  return isRewrite(res) ? new URL(getRewrittenUrl(res)!).pathname : null;
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("maintenance mode off", () => {
  it("leaves every page alone when the switch is unset", () => {
    for (const path of ["/", "/signin", "/signup"]) {
      const res = get(path);
      expect(isRewrite(res)).toBe(false);
      expect(res.headers.get("location")).toBeNull();
    }
  });

  it("only switches on for the exact value 'on'", () => {
    vi.stubEnv("MAINTENANCE_MODE", "true");
    expect(isRewrite(get("/"))).toBe(false);
  });

  it("keeps the existing sign-in redirect for protected pages", () => {
    const location = new URL(get("/workflows").headers.get("location")!);
    expect(location.pathname).toBe("/signin");
  });

  it("sends a stale /maintenance visit back home", () => {
    const location = new URL(get("/maintenance").headers.get("location")!);
    expect(location.pathname).toBe("/");
  });
});

describe("maintenance mode on", () => {
  beforeEach(() => {
    vi.stubEnv("MAINTENANCE_MODE", "on");
    vi.stubEnv("MAINTENANCE_BYPASS_TOKEN", TOKEN);
  });

  it("shows the maintenance page in place of the site, auth and studio", () => {
    for (const path of [
      "/",
      "/signin",
      "/signup",
      "/auth/callback",
      "/workflows",
      "/workflows/abc123",
      "/settings",
    ]) {
      const res = get(path);
      expect(rewrittenPath(res), path).toBe("/maintenance");
      // A rewrite keeps the visitor's URL; a redirect would not.
      expect(res.headers.get("location"), path).toBeNull();
    }
  });

  it("does not let the UI cookie alone through to the studio", () => {
    expect(rewrittenPath(get("/workflows", "agentmesh_ui=1"))).toBe(
      "/maintenance",
    );
  });

  it("keeps the legal pages and the maintenance page itself public", () => {
    for (const path of [
      "/terms",
      "/privacy",
      "/refund-policy",
      "/maintenance",
    ]) {
      const res = get(path);
      expect(isRewrite(res), path).toBe(false);
      expect(res.headers.get("location"), path).toBeNull();
    }
  });

  it("sets an HttpOnly bypass cookie for the right token and strips it from the URL", () => {
    const res = get(`/workflows?bypass=${TOKEN}&tab=runs`);
    const location = new URL(res.headers.get("location")!);
    expect(location.pathname).toBe("/workflows");
    expect(location.searchParams.get("bypass")).toBeNull();
    expect(location.searchParams.get("tab")).toBe("runs");

    const setCookie = res.headers.get("set-cookie")!;
    expect(setCookie).toContain(`${COOKIE}=${TOKEN}`);
    expect(setCookie.toLowerCase()).toContain("httponly");
    expect(setCookie.toLowerCase()).toContain("secure");
  });

  it("treats a wrong token as no token", () => {
    const res = get("/?bypass=nope");
    expect(rewrittenPath(res)).toBe("/maintenance");
    expect(res.headers.get("set-cookie")).toBeNull();
  });

  it("lets a bypassed browser through to the real site", () => {
    const res = get("/", `${COOKIE}=${TOKEN}`);
    expect(isRewrite(res)).toBe(false);
    expect(res.headers.get("location")).toBeNull();
  });

  it("still applies the sign-in check behind the bypass", () => {
    const location = new URL(
      get("/workflows", `${COOKIE}=${TOKEN}`).headers.get("location")!,
    );
    expect(location.pathname).toBe("/signin");
  });

  it("rejects a cookie from a rotated token", () => {
    expect(rewrittenPath(get("/", `${COOKIE}=old-token`))).toBe("/maintenance");
  });

  it("clears the bypass with ?bypass=off", () => {
    const res = get("/?bypass=off", `${COOKIE}=${TOKEN}`);
    expect(new URL(res.headers.get("location")!).search).toBe("");
    expect(res.headers.get("set-cookie")).toMatch(
      new RegExp(`${COOKIE}=;.*(Max-Age=0|Expires=Thu, 01 Jan 1970)`, "i"),
    );
  });

  it("admits nobody when no bypass token is configured", () => {
    vi.stubEnv("MAINTENANCE_BYPASS_TOKEN", "");
    expect(rewrittenPath(get("/?bypass="))).toBe("/maintenance");
    expect(rewrittenPath(get("/", `${COOKIE}=`))).toBe("/maintenance");
  });
});

describe("maintenance matcher", () => {
  it("runs on every page", () => {
    for (const url of [
      "/",
      "/signin",
      "/signup",
      "/auth/callback",
      "/maintenance",
    ]) {
      expect(unstable_doesMiddlewareMatch({ config, url }), url).toBe(true);
    }
  });

  it("skips the API proxy and static files the maintenance page needs", () => {
    for (const url of [
      "/api/auth/me",
      "/_next/static/chunks/app.js",
      "/logo.png",
      "/favicon.ico",
      "/opengraph-image.png",
      "/.well-known/agent-card.json",
      "/llms.txt",
    ]) {
      expect(unstable_doesMiddlewareMatch({ config, url }), url).toBe(false);
    }
  });
});
