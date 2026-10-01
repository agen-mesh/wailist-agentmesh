import { describe, expect, it } from "vitest";
import { TENDRIL_WORKFLOW } from "./data";
import { isValidConnection } from "./portUtils";

describe("TENDRIL_WORKFLOW", () => {
  const byId = new Map(TENDRIL_WORKFLOW.nodes.map((n) => [n.id, n]));
  const flow = TENDRIL_WORKFLOW.edges.filter((e) => e.kind === "flow");

  it("runs the full lease lifecycle in order: topup, rent, jobs, analyst, release", () => {
    const order = ["tw1", "tw2", "tw3", "tw4", "tw5", "tw6", "tw8", "tw9"];
    expect(flow.map((e) => [e.from, e.to])).toEqual(
      order.slice(0, -1).map((id, i) => [id, order[i + 1]]),
    );
    expect(order.map((id) => byId.get(id)!.tendrilAction ?? byId.get(id)!.type)).toEqual([
      "trigger", "topup", "rent", "run", "run", "agent", "release", "end",
    ]);
  });

  it("tops up exactly for the rent it reserves", () => {
    const topup = byId.get("tw2")!;
    const rent = byId.get("tw3")!;
    // Sizing the topup to the rent is what lets any machine price work.
    expect(topup.tendrilCoverHours).toBe(rent.tendrilHours);
    // Tendril's live minimum topup is $0.10 (GET /platform).
    expect(parseFloat(topup.tendrilAmount!)).toBeGreaterThanOrEqual(0.1);
    // Rent must pick the same cheapest machine the topup priced.
    expect(rent.tendrilNodeId).toBeUndefined();
  });

  it("gives each job a self-contained Python payload", () => {
    for (const id of ["tw4", "tw5"]) {
      const payload = byId.get(id)!.customParams?.find((p) => p.name === "payload")?.value ?? "";
      expect(payload).toContain("print(");
      // A backslash in the TS template literal would be unescaped before
      // Python ever sees it.
      expect(payload).not.toMatch(/[\\\u0000]/);
    }
  });

  it("attaches a platform-key model to the analyst", () => {
    const attach = TENDRIL_WORKFLOW.edges.find((e) => e.kind === "attach")!;
    expect([attach.from, attach.to, attach.toPort]).toEqual(["tw7", "tw6", "model"]);
    expect(byId.get("tw7")!.keyMode).toBe("platform");
  });

  it("only uses connections the canvas itself allows", () => {
    for (const e of TENDRIL_WORKFLOW.edges) {
      const from = byId.get(e.from)!;
      const to = byId.get(e.to)!;
      const fromPort = e.kind === "attach" ? "top" : "out";
      expect(isValidConnection(from, fromPort, to, e.toPort ?? "in")).toBe(true);
    }
  });
});
