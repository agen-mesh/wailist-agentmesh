import { describe, it, expect } from "vitest";
import { damp, PULL_THRESHOLD_PX } from "./PullToRefresh";

// The resistance curve, tested without a touchscreen.
//
// What matters here is not the exact pixel values but the two properties that
// make the gesture usable: it must never trigger during an ordinary scroll, and
// it must be reachable without an absurd drag. Both are asserted as ranges
// rather than exact numbers, so tuning the curve does not break the tests while
// changing its behaviour silently would.
describe("damp", () => {
  it("gives nothing back for an upward or zero pull", () => {
    expect(damp(0)).toBe(0);
    expect(damp(-40)).toBe(0);
  });

  it("stays under the threshold for a short drag", () => {
    // A few pixels of slop at the top of a list is someone starting to scroll,
    // not someone asking to refresh. Arming here would fire the gesture by
    // accident constantly.
    expect(damp(8)).toBeLessThan(PULL_THRESHOLD_PX);
    expect(damp(30)).toBeLessThan(PULL_THRESHOLD_PX);
  });

  it("arms within a comfortable thumb travel", () => {
    // ~120px is a relaxed drag on an 812px-tall screen. If the curve ever needs
    // more than that, the gesture has become work.
    expect(damp(120)).toBeGreaterThanOrEqual(PULL_THRESHOLD_PX);
  });

  it("is monotonic, so the indicator never moves backwards mid-pull", () => {
    let previous = -1;
    for (let dy = 0; dy <= 400; dy += 7) {
      const value = damp(dy);
      expect(value).toBeGreaterThanOrEqual(previous);
      previous = value;
    }
  });

  it("flattens rather than tracking the finger one to one", () => {
    // The point of the curve: doubling the drag must not double the travel, or
    // a long pull hauls the indicator down the whole screen.
    expect(damp(400)).toBeLessThan(damp(200) * 2);
    expect(damp(400)).toBeLessThan(400);
  });
});
