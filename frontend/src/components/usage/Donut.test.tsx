import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { Donut, centreFontSize } from "./Donut";

afterEach(cleanup);

// A digit is taken as 0.58em wide in the sans face.
const width = (label: string, size: number, thickness: number) =>
  label.length * 0.58 * centreFontSize(label, size, thickness);

describe("the donut's centre figure", () => {
  it("never grows past 12% of the ring", () => {
    expect(centreFontSize("$1", 120, 16)).toBeCloseTo(14.4);
    // Desktop's ring keeps the size it always had.
    expect(centreFontSize("136.09", 168, 22)).toBeCloseTo(20.16);
  });

  it("shrinks a longer figure rather than letting it cross the ring", () => {
    expect(centreFontSize("$1,234,567.89", 120, 16)).toBeLessThan(
      centreFontSize("$136.09", 120, 16),
    );
    for (const label of ["$136.09", "$12,345.67", "$1,234,567.89"]) {
      // The phone's hole is 88 units across.
      expect(width(label, 120, 16)).toBeLessThanOrEqual(88 * 0.78 + 1e-9);
    }
  });

  it("draws the figure at the fitted size, and reads its label", () => {
    const { container } = render(
      <Donut
        segments={[{ label: "x402", value: 1, color: "red" }]}
        size={120}
        thickness={16}
        centerLabel="$1,234,567.89"
        ariaLabel="$1,234,567.89 spent"
      />,
    );
    const text = container.querySelector("text")!;
    expect(Number(text.getAttribute("font-size"))).toBeCloseTo(
      centreFontSize("$1,234,567.89", 120, 16),
    );
    expect(container.querySelector("svg")!.getAttribute("aria-label")).toBe(
      "$1,234,567.89 spent",
    );
  });
});
