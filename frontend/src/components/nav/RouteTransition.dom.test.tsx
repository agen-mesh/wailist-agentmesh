import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

const nav = vi.hoisted(() => ({ pathname: "/workflows" }));
vi.mock("next/navigation", () => ({ usePathname: () => nav.pathname }));
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => true }));

import { RouteTransition } from "./RouteTransition";

function stubReducedMotion(reduce: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((q: string) => ({ matches: reduce && q.includes("reduce") })),
  );
}

function navigate() {
  nav.pathname = "/workflows";
  const view = render(<RouteTransition>page</RouteTransition>);
  nav.pathname = "/workflows/abc";
  view.rerender(<RouteTransition>page</RouteTransition>);
  return view.container.querySelector(".route-frame > div")!;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("RouteTransition", () => {
  it("marks the arriving screen with its direction", () => {
    stubReducedMotion(false);
    expect(navigate().className).toBe("route-enter-fwd");
  });

  // No animation, so no animationend would ever remove the class, and the
  // class keeps the frame clipped.
  it("adds nothing when reduced motion is on", () => {
    stubReducedMotion(true);
    expect(navigate().className).toBe("");
  });

  // Turning reduced motion on mid-transition cancels the animation, so no
  // animationend arrives to take the class off -- and the class clips the
  // frame for as long as it stays.
  it("drops the class when the animation is cancelled", () => {
    stubReducedMotion(false);
    const screen = navigate();
    expect(screen.className).toBe("route-enter-fwd");
    screen.dispatchEvent(new Event("animationcancel", { bubbles: true }));
    expect(screen.className).toBe("");
  });
});
