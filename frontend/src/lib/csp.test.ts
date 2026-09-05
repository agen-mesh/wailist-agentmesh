import { describe, it, expect } from "vitest";
import { apiOrigin, buildCsp, websocketOrigin } from "./csp";

// Reading one directive out of the policy string, so a test can assert on the
// part it cares about instead of matching the whole thing.
function directive(policy: string, name: string): string {
  const found = policy
    .split(";")
    .map((d) => d.trim())
    .find((d) => d === name || d.startsWith(name + " "));
  if (!found) throw new Error(`policy has no ${name}: ${policy}`);
  return found;
}

const API = "https://api.agent-mesh.app";

describe("apiOrigin", () => {
  it("keeps the origin and drops everything else", () => {
    expect(apiOrigin("https://api.example.com/v1/workflows?x=1")).toBe(
      "https://api.example.com",
    );
  });

  it("keeps a non-default port, which is a different origin", () => {
    expect(apiOrigin("http://localhost:8080")).toBe("http://localhost:8080");
  });

  // An unset API URL is a supported build: lib/api.ts runs on mock data.
  it("returns null when there is no API configured", () => {
    expect(apiOrigin("")).toBeNull();
    expect(apiOrigin("   ")).toBeNull();
    expect(apiOrigin(undefined)).toBeNull();
  });

  it("returns null rather than throwing on a malformed URL", () => {
    expect(apiOrigin("not a url")).toBeNull();
  });
});

describe("websocketOrigin", () => {
  // The Tendril terminal derives its socket URL exactly this way.
  it("maps https to wss and http to ws", () => {
    expect(websocketOrigin("https://api.example.com")).toBe(
      "wss://api.example.com",
    );
    expect(websocketOrigin("http://localhost:8080")).toBe(
      "ws://localhost:8080",
    );
  });
});

describe("buildCsp", () => {
  it("admits the API over both https and wss", () => {
    const connect = directive(buildCsp(API), "connect-src");
    expect(connect).toContain("https://api.agent-mesh.app");
    // The half that is easy to forget. Without it the Tendril terminal never
    // connects, and the only evidence is a console message on a phone.
    expect(connect).toContain("wss://api.agent-mesh.app");
  });

  it("admits the bundle's own origin, which is where every asset lives", () => {
    expect(directive(buildCsp(API), "connect-src")).toContain("'self'");
  });

  it("does not admit anything else", () => {
    // The whole point of the directive: a compromised dependency can reach our
    // backend or nothing.
    //
    // Compared token by token rather than by substring, because the allowed
    // origin legitimately CONTAINS "https:" -- a substring check here passes
    // or fails for the wrong reason.
    const sources = directive(buildCsp(API), "connect-src")
      .split(/\s+/)
      .slice(1);
    expect(sources).toEqual([
      "'self'",
      "https://api.agent-mesh.app",
      "wss://api.agent-mesh.app",
    ]);
    // A bare scheme or a wildcard would re-open everything the directive is
    // there to shut.
    for (const wildcard of ["*", "https:", "http:", "data:", "'unsafe-eval'"]) {
      expect(sources).not.toContain(wildcard);
    }
  });

  it("still produces a valid policy with no API configured", () => {
    const policy = buildCsp("");
    expect(directive(policy, "connect-src")).toBe("connect-src 'self'");
    expect(policy).not.toContain("null");
    expect(policy).not.toContain("undefined");
  });

  it("closes the cheap injection routes outright", () => {
    const policy = buildCsp(API);
    expect(directive(policy, "object-src")).toBe("object-src 'none'");
    expect(directive(policy, "base-uri")).toBe("base-uri 'self'");
    expect(directive(policy, "form-action")).toBe("form-action 'self'");
  });

  it("closes font-src, since the fonts are self-hosted", () => {
    // layout.tsx deliberately self-hosts Geist. Pinning this means a future
    // dependency cannot quietly reintroduce a fetch to a font CDN.
    expect(directive(buildCsp(API), "font-src")).toBe("font-src 'self'");
  });

  // Documenting the limitation rather than pretending it away. If somebody
  // later removes 'unsafe-inline' from script-src, the static export stops
  // hydrating -- and this test is where they should find out why.
  it("keeps the inline allowances a static export cannot do without", () => {
    const policy = buildCsp(API);
    expect(directive(policy, "script-src")).toContain("'unsafe-inline'");
    expect(directive(policy, "style-src")).toContain("'unsafe-inline'");
  });

  // Found by actually loading the built bundle: Chrome logs "the directive
  // 'frame-ancestors' is ignored when delivered via a <meta> element" on every
  // launch. This policy is only ever delivered as a meta tag -- there is no
  // server to send a header -- so the directive is pure noise, and noise on
  // startup is how real errors get scrolled past.
  it("omits the directives a meta-delivered policy cannot use", () => {
    const policy = buildCsp(API);
    for (const ignored of ["frame-ancestors", "sandbox", "report-uri"]) {
      expect(policy).not.toContain(ignored);
    }
  });

  it("is a single header-shaped line", () => {
    const policy = buildCsp(API);
    expect(policy).not.toContain("\n");
    expect(policy.startsWith("default-src 'self'")).toBe(true);
  });
});
