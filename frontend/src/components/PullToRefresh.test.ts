import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { damp, PULL_THRESHOLD_PX, pullProgress } from "./PullToRefresh";

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

// The ring fills with the pull. Nothing turns until the refresh starts: an
// arrow that rotated as you dragged was read as the indicator spinning oddly.
describe("pullProgress", () => {
  it("is empty at rest and full once the pull will fire", () => {
    expect(pullProgress(0)).toBe(0);
    expect(pullProgress(PULL_THRESHOLD_PX)).toBe(1);
  });

  it("fills in step with the pull, never past full", () => {
    expect(pullProgress(PULL_THRESHOLD_PX / 2)).toBe(0.5);
    expect(pullProgress(PULL_THRESHOLD_PX * 3)).toBe(1);
    expect(pullProgress(-20)).toBe(0);
    let previous = -1;
    for (let offset = 0; offset <= PULL_THRESHOLD_PX; offset += 4) {
      expect(pullProgress(offset)).toBeGreaterThanOrEqual(previous);
      previous = pullProgress(offset);
    }
  });
});

// The spin must never sit on .ptr-indicator. That element is positioned with an
// inline transform, and CSS applies rotation around its origin after that
// translate, so the bubble orbited the top of the list instead of spinning. It
// is asserted against the stylesheet because jsdom does not apply it.
describe("the spin is on the ring, not the bubble", () => {
  // Resolved from the project root: vitest does not give this file a
  // file:// import.meta.url.
  const css = readFileSync(
    resolve(process.cwd(), "src/app/responsive.css"),
    "utf8",
  );
  const block = (selector: string) => {
    const at = css.indexOf(selector + " {");
    expect(at, `${selector} missing`).toBeGreaterThan(-1);
    return css.slice(at, css.indexOf("}", at));
  };

  it("animates the ring", () => {
    expect(block(".ptr-indicator__ring[data-spinning]")).toContain(
      "animation: ptr-spin",
    );
  });

  it("never animates the positioned bubble", () => {
    expect(block(".ptr-indicator[data-spinning]")).not.toContain("animation");
    expect(block(".ptr-indicator")).not.toContain("animation");
  });
});
