import { afterEach, describe, expect, it, vi } from "vitest";
import {
  gateRoute,
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

describe("gateRoute", () => {
  it("lets a route through when signed in", () => {
    const route = { href: "/workflows/app?id=wf-1", replace: false };
    expect(gateRoute(route, true)).toBe(route);
  });

  it("sends a protected route through sign-in when signed out", () => {
    expect(
      gateRoute({ href: "/workflows/app?id=wf-1", replace: false }, false),
    ).toEqual({
      href: "/signin?next=%2Fworkflows%2Fapp%3Fid%3Dwf-1",
      replace: true,
    });
  });

  it("leaves a sign-in result alone when signed out", () => {
    const route = { href: "/signin?error=cancelled", replace: true };
    expect(gateRoute(route, false)).toBe(route);
  });
});
