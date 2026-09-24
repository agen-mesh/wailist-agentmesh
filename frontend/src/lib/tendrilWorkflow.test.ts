import { describe, expect, it } from "vitest";
import { TENDRIL_WORKFLOW } from "./data";
import { isValidConnection } from "./portUtils";

describe("TENDRIL_WORKFLOW", () => {
  const byId = new Map(TENDRIL_WORKFLOW.nodes.map((n) => [n.id, n]));

  it("is trigger -> conditional topup -> run -> end on native tendril nodes", () => {
    const order = ["tw1", "tw2", "tw3", "tw4"];
    expect(TENDRIL_WORKFLOW.edges.map((e) => [e.from, e.to])).toEqual(
      order.slice(0, -1).map((id, i) => [id, order[i + 1]]),
    );

    const topup = byId.get("tw2")!;
    expect(topup.type).toBe("tendril");
    expect(topup.tendrilAction).toBe("topup");
    // Tendril's live minimum topup is $0.10 (GET /platform).
    expect(parseFloat(topup.tendrilAmount!)).toBeGreaterThanOrEqual(0.1);
    // Without a threshold the backend tops up on every run.
    expect(parseFloat(topup.tendrilMinBalance!)).toBeGreaterThan(0);

    const run = byId.get("tw3")!;
    expect(run.type).toBe("tendril");
    expect(run.tendrilAction).toBe("run");
    const payload = run.customParams?.find((p) => p.name === "payload");
    expect(payload?.value).toContain("print(");
  });

  it("only uses connections the canvas itself allows", () => {
    for (const e of TENDRIL_WORKFLOW.edges) {
      const from = byId.get(e.from)!;
      const to = byId.get(e.to)!;
      expect(isValidConnection(from, "out", to, e.toPort ?? "in")).toBe(true);
    }
  });
});
