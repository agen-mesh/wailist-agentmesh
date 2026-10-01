import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { renderToString } from "react-dom/server";

const device = vi.hoisted(() => ({ native: false }));
vi.mock("@/lib/nativeAuth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/nativeAuth")>()),
  get IS_NATIVE() { return device.native; },
}));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  workflows: { list: async () => [] },
}));
vi.mock("@/lib/credits/store", () => {
  const refreshBalance = async () => {};
  return { useCredits: () => ({ balanceUSD: 0, balanceKnown: true, refreshBalance }) };
});

import { WorkflowsPage } from "./WorkflowsPage";

// jsdom does not evaluate media queries. Activate matching rules from the
// actual stylesheet so a pointer-only hide still fails this regression.
function applyActionStyles() {
  const css = readFileSync("src/app/globals.css", "utf8");
  const source = document.createElement("style");
  source.textContent = css.slice(css.indexOf(".wf-actions {"), css.indexOf(".wf-controls {"));
  document.head.append(source);
  function activeRules(rules: CSSRuleList): string {
    return Array.from(rules).map((rule) => {
      if (rule instanceof CSSMediaRule) {
        return window.matchMedia(rule.conditionText).matches ? activeRules(rule.cssRules) : "";
      }
      return rule.cssText;
    }).join("\n");
  }
  const active = document.createElement("style");
  active.textContent = activeRules(source.sheet!.cssRules);
  source.remove();
  document.head.append(active);
  return active;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "maxTouchPoints");
  device.native = false;
});

describe("workflow actions follow the device policy", () => {
  it("keeps actions out of the server paint until device classification", () => {
    const element = document.createElement("div");
    element.innerHTML = renderToString(<WorkflowsPage />);
    expect((element.querySelector(".wf-actions") as HTMLElement).style.visibility).toBe("hidden");
  });

  it.each([
    { name: "touch desktop", platform: "Windows", mobile: false, native: false, allowed: true },
    { name: "phone", platform: "Android", mobile: true, native: false, allowed: false },
    { name: "tablet", platform: "Android", mobile: false, native: false, allowed: false },
    { name: "native client with desktop signals", platform: "Windows", mobile: false, native: true, allowed: false },
  ])("$name", async ({ platform, mobile, native, allowed }) => {
    device.native = native;
    Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: 5 });
    Object.defineProperty(navigator, "userAgentData", { configurable: true, value: { platform, mobile } });
    vi.stubGlobal("matchMedia", vi.fn((media: string) => ({
      media, matches: media.includes("pointer: coarse") || media.includes("hover: none"),
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    })));
    const style = applyActionStyles();
    try {
      const view = render(<WorkflowsPage />);
      await screen.findByText(/^no workflows yet/i);
      const actions = view.container.querySelector(".wf-actions");
      if (allowed) {
        expect(actions).not.toBeNull();
        expect(getComputedStyle(actions!).display).toBe("flex");
      } else {
        expect(actions).toBeNull();
      }
      for (const name of [/^Import$/, /^Run demo workflow/, /New workflow/]) {
        expect(screen.queryByRole("button", { name }) !== null).toBe(allowed);
      }
    } finally {
      style.remove();
      delete (navigator as Navigator & { userAgentData?: unknown }).userAgentData;
    }
  });
});
