import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({ handheld: true, replace: vi.fn() }));

vi.mock("@/hooks/useIsHandheld", () => ({
  useIsHandheld: () => state.handheld,
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace }),
}));

import { DesktopOnlyRoute } from "./DesktopOnlyRoute";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  state.handheld = true;
});

describe("DesktopOnlyRoute", () => {
  it("sends a handheld to Workflows instead of rendering the screen", () => {
    render(
      <DesktopOnlyRoute>
        <p>Bazaar</p>
      </DesktopOnlyRoute>,
    );
    expect(state.replace).toHaveBeenCalledWith("/workflows");
    // Not even one frame of a screen that is about to disappear.
    expect(screen.queryByText("Bazaar")).toBeNull();
  });

  it("replaces rather than pushes, so Back does not bounce", () => {
    render(
      <DesktopOnlyRoute>
        <p>Bazaar</p>
      </DesktopOnlyRoute>,
    );
    expect(state.replace).toHaveBeenCalledTimes(1);
  });

  it("renders the screen untouched on a desktop", () => {
    state.handheld = false;
    render(
      <DesktopOnlyRoute>
        <p>Bazaar</p>
      </DesktopOnlyRoute>,
    );
    expect(screen.getByText("Bazaar")).toBeTruthy();
    expect(state.replace).not.toHaveBeenCalled();
  });

  it("can be pointed somewhere other than Workflows", () => {
    render(
      <DesktopOnlyRoute to="/account">
        <p>Bazaar</p>
      </DesktopOnlyRoute>,
    );
    expect(state.replace).toHaveBeenCalledWith("/account");
  });
});
