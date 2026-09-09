import { describe, it, expect } from "vitest";
import { enterDirection } from "./RouteTransition";

// The direction rule, tested without a DOM, a router, or a phone. The animation
// itself is a keyframe in responsive.css; what is worth pinning is the decision
// about which way a screen came from, because getting it backwards is the one
// failure that actively misleads -- an arriving screen that slides the wrong way
// tells the user they went back when they went deeper.
describe("enterDirection", () => {
  it("reads a deeper route as forward", () => {
    expect(enterDirection("/workflows", "/workflows/abc")).toBe("forward");
    expect(enterDirection("/workflows/abc", "/workflows/abc/geofence")).toBe(
      "forward",
    );
  });

  it("reads a shallower route as back", () => {
    expect(enterDirection("/workflows/abc", "/workflows")).toBe("back");
    expect(enterDirection("/workflows/abc/geofence", "/workflows/abc")).toBe(
      "back",
    );
  });

  it("reads a move between peers as forward", () => {
    // Tab roots are the same depth and have no "back" between them. Forward is
    // the right default: a sideways move is still an arrival.
    expect(enterDirection("/workflows", "/bazaar")).toBe("forward");
    expect(enterDirection("/usage", "/billing")).toBe("forward");
  });

  it("ignores trailing and duplicated slashes", () => {
    // Depth counts non-empty segments, so "/workflows/" must not read as deeper
    // than "/workflows" and send a screen the wrong way over a cosmetic
    // difference in the path.
    expect(enterDirection("/workflows", "/workflows/")).toBe("forward");
    expect(enterDirection("/workflows/", "/workflows")).toBe("forward");
    expect(enterDirection("/workflows//abc", "/workflows")).toBe("back");
  });

  it("treats the root as the shallowest thing there is", () => {
    expect(enterDirection("/workflows", "/")).toBe("back");
    expect(enterDirection("/", "/workflows")).toBe("forward");
  });
});
