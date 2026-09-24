import { describe, expect, it } from "vitest";
import { APP_NAV_ITEMS, HANDHELD_TAB_ITEMS, isTabRoot } from "./nav";

describe("isTabRoot", () => {
  it("is true for every handheld tab's own route", () => {
    for (const item of HANDHELD_TAB_ITEMS) {
      expect(isTabRoot(item.href!)).toBe(true);
    }
  });

  it("is false for a screen pushed on top of a tab", () => {
    expect(isTabRoot("/workflows/wf-triage")).toBe(false);
    expect(isTabRoot("/workflows/wf-triage/geofence")).toBe(false);
    expect(isTabRoot("/account/settings")).toBe(false);
  });

  it("is false for a route with no tab, and for the marketing page", () => {
    expect(isTabRoot("/bazaar")).toBe(false);
    expect(isTabRoot("/billing")).toBe(false);
    expect(isTabRoot("/")).toBe(false);
  });

  it("does not treat a trailing slash as the tab root", () => {
    expect(isTabRoot("/workflows/")).toBe(false);
  });
});

describe("the handheld tabs", () => {
  const labels = () => HANDHELD_TAB_ITEMS.map((i) => i.label);

  // Usage is something you check; Credits is somewhere you go to top up,
  // reached from the Workflows "+" and from Account.
  it("carry Usage rather than Credits", () => {
    expect(labels()).toEqual(["Workflows", "Activity", "Usage", "Account"]);
    expect(HANDHELD_TAB_ITEMS.find((i) => i.label === "Usage")?.href).toBe(
      "/usage",
    );
    // Being a tab root is what puts the bottom bar on that screen.
    expect(isTabRoot("/usage")).toBe(true);
  });

  it("leave out the Bazaar, which is for building on a computer", () => {
    expect(labels()).not.toContain("Bazaar");
  });
});

describe("desktop-only routes", () => {
  it("marks the Bazaar, and nothing else, as desktop only", () => {
    expect(
      APP_NAV_ITEMS.filter((i) => i.desktopOnly).map((i) => i.label),
    ).toEqual(["Bazaar"]);
  });

  it("still offers the Bazaar on a desktop", () => {
    expect(APP_NAV_ITEMS.map((i) => i.label)).toContain("Bazaar");
  });
});
