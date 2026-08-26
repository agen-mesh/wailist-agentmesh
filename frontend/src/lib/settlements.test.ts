import { describe, it, expect, beforeEach } from "vitest";
import { listSettlements, recordSettlements } from "./settlements";
import type { Settlement } from "@/lib/types";

function settlement(partial: Partial<Settlement>): Settlement {
  return {
    ts: "2026-08-07T09:00:00.000Z",
    endpoint: "x402 endpoint",
    amountAlgo: 0.05,
    txId: "",
    explorerURL: "",
    workflowId: "wf1",
    ...partial,
  };
}

describe("recordSettlements", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("drops a duplicate of an already-stored tx id", () => {
    recordSettlements("u1", [settlement({ txId: "aa" })]);
    recordSettlements("u1", [settlement({ txId: "aa", amountAlgo: 0.1 })]);
    expect(listSettlements("u1")).toHaveLength(1);
  });

  it("does not drop a settlement with no tx id as if it were a duplicate", () => {
    // A v2/relay settlement without a tx id is still a real, billed charge --
    // "" must never be treated as an id that was "already seen".
    recordSettlements("u1", [settlement({ txId: "" })]);
    recordSettlements("u1", [settlement({ txId: "" })]);
    expect(listSettlements("u1")).toHaveLength(2);
  });

  it("keeps multiple no-tx-id settlements within the same incoming batch", () => {
    recordSettlements("u1", [
      settlement({ txId: "", amountAlgo: 0.01 }),
      settlement({ txId: "", amountAlgo: 0.02 }),
    ]);
    expect(listSettlements("u1")).toHaveLength(2);
  });

  it("still dedupes a real tx id repeated within the same incoming batch", () => {
    recordSettlements("u1", [
      settlement({ txId: "aa" }),
      settlement({ txId: "aa" }),
    ]);
    expect(listSettlements("u1")).toHaveLength(1);
  });
});
