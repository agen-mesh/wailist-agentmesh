import { afterEach, describe, expect, it, vi } from "vitest";
import {
  navigateInApp,
  setInAppNavigator,
  takePendingRoute,
} from "./nativeNav";

afterEach(() => {
  setInAppNavigator(null);
  takePendingRoute();
});

describe("navigateInApp", () => {
  it("hands the route to the router when one can take it", () => {
    const router = vi.fn(() => true);
    setInAppNavigator(router);

    navigateInApp("/workflows/app?id=wf-1");

    expect(router).toHaveBeenCalledWith({
      href: "/workflows/app?id=wf-1",
      replace: false,
    });
    expect(takePendingRoute()).toBeNull();
  });

  it("holds the route while the router cannot take it yet", () => {
    setInAppNavigator(() => false);

    navigateInApp("/signin?error=cancelled", { replace: true });

    expect(takePendingRoute()).toEqual({
      href: "/signin?error=cancelled",
      replace: true,
    });
    // Once only.
    expect(takePendingRoute()).toBeNull();
  });

  it("holds the route when nothing is registered, as on a cold start", () => {
    navigateInApp("/workflows/app?id=wf-1");
    expect(takePendingRoute()?.href).toBe("/workflows/app?id=wf-1");
  });

  it("keeps only the latest held route", () => {
    navigateInApp("/workflows/app?id=wf-1");
    navigateInApp("/workflows/app?id=wf-2");
    expect(takePendingRoute()?.href).toBe("/workflows/app?id=wf-2");
  });
});
