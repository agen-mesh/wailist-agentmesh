import { describe, expect, it } from "vitest";
import { WHITECHAIN_WORKFLOW } from "./data";
import { isValidConnection } from "./portUtils";

describe("WHITECHAIN_WORKFLOW", () => {
  const byId = new Map(WHITECHAIN_WORKFLOW.nodes.map((n) => [n.id, n]));

  it("reads the chain, then asks a paid-tool agent, then ends", () => {
    const flow = WHITECHAIN_WORKFLOW.edges.filter((e) => e.kind === "flow");
    expect(flow.map((e) => [e.from, e.to])).toEqual([["wc1", "wc2"], ["wc2", "wc3"], ["wc3", "wc6"]]);
    expect(byId.get("wc2")!.template).toBe("whitechain_account");
    expect(byId.get("wc2")!.config?.wcAddress).toMatch(/^0x[0-9a-fA-F]{40}$/);
  });

  it("pays a Whitechain x402 tool at or above the facilitator's 0.01 USDT minimum", () => {
    const tool = byId.get("wc5")!;
    expect(tool.type).toBe("tool402");
    expect(parseFloat(tool.price!)).toBeGreaterThanOrEqual(0.01);
  });

  it("only uses connections the canvas itself allows", () => {
    for (const e of WHITECHAIN_WORKFLOW.edges) {
      const from = byId.get(e.from)!;
      const to = byId.get(e.to)!;
      const fromPort = e.kind === "attach" ? "top" : "out";
      expect(isValidConnection(from, fromPort, to, e.toPort ?? "in")).toBe(true);
    }
  });
});
