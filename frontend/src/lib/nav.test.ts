import { describe, expect, it } from "vitest";
import { HANDHELD_TAB_ITEMS, isTabRoot } from "./nav";

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
    expect(isTabRoot("/usage")).toBe(false);
    expect(isTabRoot("/billing")).toBe(false);
    expect(isTabRoot("/")).toBe(false);
  });

  it("does not treat a trailing slash as the tab root", () => {
    expect(isTabRoot("/workflows/")).toBe(false);
  });
});
