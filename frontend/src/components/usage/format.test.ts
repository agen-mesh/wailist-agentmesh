import { describe, expect, it } from "vitest";
import { centreFigure, endpointLabel, pct } from "./format";

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

// Regression: the endpoints table rendered `{r.pctOfSpend}%` unrounded. The
// fixtures in lib/data.ts pre-round to one decimal, so this only ever showed
// against a real /usage/by-endpoint response -- "38.95454763146232%" landing
// in a cell 40px wide.
describe("pct", () => {
  it("rounds a raw share to one decimal", () => {
    expect(pct(38.95454763146232)).toBe("39.0");
    expect(pct(3.844861)).toBe("3.8");
    expect(pct(0)).toBe("0.0");
    expect(pct(100)).toBe("100.0");
  });

  it("leaves an already-rounded fixture value alone", () => {
    expect(pct(26.7)).toBe("26.7");
  });

  it("does not print NaN or Infinity into the DOM", () => {
    expect(pct(NaN)).toBe("0");
    expect(pct(Infinity)).toBe("0");
  });
});

describe("endpointLabel", () => {
  it("keeps url over host for an x402 row", () => {
    expect(
      endpointLabel({
        endpoint: "https://api.weatherxm.com/v1/observations",
        host: "api.weatherxm.com",
        provider: "WeatherXM Observations",
      }),
    ).toEqual({
      primary: "https://api.weatherxm.com/v1/observations",
      secondary: "api.weatherxm.com",
    });
  });

  // The LLM case: /usage/by-endpoint falls back to the node id for BOTH
  // fields, so the cell used to print the same uuid on two lines.
  it("names an LLM row after its node instead of printing the id twice", () => {
    expect(
      endpointLabel({
        endpoint: "demo-wf-04-agent",
        host: "demo-wf-04-agent",
        provider: "Market agent",
      }),
    ).toEqual({ primary: "Market agent", secondary: "" });
  });

  it("falls back to the id when the node has no name either", () => {
    expect(
      endpointLabel({ endpoint: "node-9", host: "node-9", provider: "" }),
    ).toEqual({ primary: "node-9", secondary: "" });
  });
});
