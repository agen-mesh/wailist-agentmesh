import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({ handheld: true, pathname: "/workflows" }));

vi.mock("@/hooks/useIsHandheld", () => ({
  useIsHandheld: () => state.handheld,
}));
vi.mock("next/navigation", () => ({ usePathname: () => state.pathname }));

import { BottomNav } from "./BottomNav";

afterEach(() => {
  cleanup();
  state.handheld = true;
  state.pathname = "/workflows";
  document.body.removeAttribute("data-bottomnav");
});

describe("BottomNav", () => {
  it("offers Workflows, Activity, Bazaar and Account on a handheld", () => {
    state.pathname = "/activity";
    render(<BottomNav />);

    const links = screen.getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Workflows",
      "Activity",
      "Bazaar",
      "Account",
    ]);
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/workflows",
      "/activity",
      "/bazaar",
      "/account",
    ]);
    expect(
      screen
        .getByRole("link", { name: "Activity" })
        .getAttribute("aria-current"),
    ).toBe("page");
    expect(document.body.hasAttribute("data-bottomnav")).toBe(true);
  });

  it("shows on the Account tab too", () => {
    state.pathname = "/account";
    render(<BottomNav />);
    expect(
      screen
        .getByRole("link", { name: "Account" })
        .getAttribute("aria-current"),
    ).toBe("page");
  });

  it("hides on a pushed screen such as a workflow", () => {
    state.pathname = "/workflows/wf-1";
    render(<BottomNav />);
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(document.body.hasAttribute("data-bottomnav")).toBe(false);
  });

  it("hides on a desktop", () => {
    state.handheld = false;
    render(<BottomNav />);
    expect(screen.queryByRole("navigation")).toBeNull();
  });
});
