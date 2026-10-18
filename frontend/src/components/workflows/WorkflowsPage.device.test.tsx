import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { renderToString } from "react-dom/server";

const device = vi.hoisted(() => ({ native: false, list: vi.fn(), createShare: vi.fn() }));
vi.mock("@/lib/nativeAuth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/nativeAuth")>()),
  get IS_NATIVE() {
    return device.native;
  },
}));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  workflows: { list: device.list },
  shares: { create: device.createShare, listFor: async () => [] },
}));
vi.mock("@/lib/credits/store", () => {
  const refreshBalance = async () => {};
  return {
    useCredits: () => ({ balanceUSD: 0, balanceKnown: true, refreshBalance }),
  };
});

import { WorkflowsPage } from "./WorkflowsPage";

beforeEach(() => {
  device.list.mockResolvedValue([]);
  device.createShare.mockResolvedValue({
    share: {
      token: "k3Jm9xQpLr2sTvWyZa1b", name: "Phone workflow",
      graph: { nodes: [], edges: [] }, nodeCount: 0, edgeCount: 0,
      importCount: 0, createdAt: "2026-10-17T00:00:00Z",
    },
    redactions: {},
  });
});

// jsdom does not evaluate media queries. Activate matching rules from the
// actual stylesheet so a pointer-only hide still fails this regression.
function applyActionStyles() {
  const css = readFileSync("src/app/globals.css", "utf8");
  const source = document.createElement("style");
  source.textContent = css.slice(
    css.indexOf(".wf-actions {"),
    css.indexOf(".wf-controls {"),
  );
  document.head.append(source);
  function activeRules(rules: CSSRuleList): string {
    return Array.from(rules)
      .map((rule) => {
        if (rule instanceof CSSMediaRule) {
          return window.matchMedia(rule.conditionText).matches
            ? activeRules(rule.cssRules)
            : "";
        }
        return rule.cssText;
      })
      .join("\n");
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
  Reflect.deleteProperty(navigator, "userAgentData");
  vi.clearAllMocks();
  device.native = false;
});

describe("workflow actions follow the device policy", () => {
  it.each([false, true])("opens sharing from the handheld list without enabling import (native=%s)", async (native) => {
    device.native = native;
    device.list.mockResolvedValue([{
      id: "wf-phone", name: "Phone workflow", status: "draft", nodes: [], edges: [],
    }]);
    Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: 5 });
    Object.defineProperty(navigator, "userAgentData", {
      configurable: true, value: { platform: "Android", mobile: true },
    });
    vi.stubGlobal("matchMedia", vi.fn((media: string) => ({
      media, matches: media.includes("pointer: coarse") || media.includes("hover: none"),
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    })));

    render(<WorkflowsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "Share Phone workflow" }));

    expect(await screen.findByRole("dialog", { name: "Share workflow" })).toBeTruthy();
    expect(device.createShare).toHaveBeenCalledExactlyOnceWith("wf-phone", 0, true);
    expect(screen.queryByRole("button", { name: /^Import a link$|^New workflow$|^Run demo workflow/ })).toBeNull();
    expect(screen.getByRole("link", { name: "View usage for Phone workflow" }).getAttribute("href"))
      .toBe("/usage?workflow=wf-phone");
  });

  it("keeps actions out of the server paint until device classification", () => {
    const element = document.createElement("div");
    element.innerHTML = renderToString(<WorkflowsPage />);
    expect(
      (element.querySelector(".wf-actions") as HTMLElement).style.visibility,
    ).toBe("hidden");
  });

  it.each([
    {
      name: "touch desktop",
      platform: "Windows",
      mobile: false,
      native: false,
      allowed: true,
    },
    {
      name: "phone",
      platform: "Android",
      mobile: true,
      native: false,
      allowed: false,
    },
    {
      name: "tablet",
      platform: "Android",
      mobile: false,
      native: false,
      allowed: false,
    },
    {
      name: "native client with desktop signals",
      platform: "Windows",
      mobile: false,
      native: true,
      allowed: false,
    },
  ])("$name", async ({ platform, mobile, native, allowed }) => {
    device.native = native;
    Object.defineProperty(navigator, "maxTouchPoints", {
      configurable: true,
      value: 5,
    });
    Object.defineProperty(navigator, "userAgentData", {
      configurable: true,
      value: { platform, mobile },
    });
    vi.stubGlobal(
      "matchMedia",
      vi.fn((media: string) => ({
        media,
        matches:
          media.includes("pointer: coarse") || media.includes("hover: none"),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    );
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
      for (const name of [
        /^Import a link$/,
        /^Run demo workflow/,
        /New workflow/,
      ]) {
        expect(screen.queryByRole("button", { name }) !== null).toBe(allowed);
      }
    } finally {
      style.remove();
      delete (navigator as Navigator & { userAgentData?: unknown })
        .userAgentData;
    }
  });
});
