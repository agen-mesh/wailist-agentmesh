import { describe, expect, it } from "vitest";
import { TENDRIL_WORKFLOW } from "./data";
import { isValidConnection } from "./portUtils";

describe("TENDRIL_WORKFLOW", () => {
  const byId = new Map(TENDRIL_WORKFLOW.nodes.map((n) => [n.id, n]));

  it("is trigger -> run -> end on a native tendril node", () => {
    const order = ["tw1", "tw2", "tw3"];
    expect(TENDRIL_WORKFLOW.edges.map((e) => [e.from, e.to])).toEqual(
      order.slice(0, -1).map((id, i) => [id, order[i + 1]]),
    );

    // A run never draws Tendril credit, so a topup here would charge for
    // credit the workflow can't spend.
    expect(
      TENDRIL_WORKFLOW.nodes.some((n) => n.tendrilAction === "topup"),
    ).toBe(false);

    const run = byId.get("tw2")!;
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
