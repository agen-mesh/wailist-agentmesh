import { describe, expect, it } from "vitest";
import { centreFigure } from "./format";

// Exact while it fits a ring's hole; compact once it would not.
describe("centreFigure", () => {
  it("is exact below a thousand", () => {
    expect(centreFigure(136.09)).toBe("$136.09");
    expect(centreFigure(999.99)).toBe("$999.99");
  });

  it("is compact from a thousand up", () => {
    expect(centreFigure(12345.6)).toBe("$12.3K");
    expect(centreFigure(1_234_567)).toBe("$1.2M");
  });
});
