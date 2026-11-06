import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ConsolePanel } from "./ConsolePanel";
import type { LogEvent } from "./useRunTranscript";
import type { RunCosts } from "@/lib/runCosts";

const costs: RunCosts = {
  totalUsdMicros: 1_565_000,
  steps: [{ nodeId: "step", totalUsdMicros: 1_565_000,
    byKind: { x402_relay_cost: 65_000, x402_platform_fee: 1_500_000 } }],
};
const log = (status: LogEvent["status"], stepIndex = 0): LogEvent => ({
  nodeId: "step", nodeType: "tool", status, stepIndex,
  output: "result", durationMs: 10, ts: "2026-10-01T12:00:00Z",
});
function panel(logs: LogEvent[], runCosts: RunCosts | null) {
  const el = document.createElement("div");
  el.innerHTML = renderToStaticMarkup(<ConsolePanel open onToggle={() => {}}
    runId="run1" running={false} logs={logs} elapsed={1} done
    deadLetters={[]} costs={runCosts} onResume={() => {}} />);
  return el;
}

describe("console cost integration", () => {
  it("preserves the partial-answer warning alongside the charge", () => {
    const el = panel([log("degraded")], costs);
    expect(el.textContent).toContain("1 step failed, this answer is partial");
    expect(el.textContent).toContain("0/1 nodes succeeded");
    expect(el.textContent).toContain("$1.57 charged");
    expect(el.textContent).not.toContain("run complete");
    expect(el.textContent).not.toContain("run failed");
  });

  it("shows a resumed node's aggregate charge only on its last attempt", () => {
    const el = panel([log("failed"), log("success", 1)], costs);
    expect(el.textContent).toContain("run complete");
    expect(el.textContent).toContain("1/1 nodes succeeded");
    expect(el.textContent).not.toContain("run failed");
    const charges = el.querySelectorAll('[title="platform fee $1.50 · API cost $0.07"]');
    expect(charges).toHaveLength(1);
    expect(charges[0].textContent).toBe("· $1.57");
    expect(el.textContent).toContain("$1.57 charged");
  });

  it("keeps the existing summary when an older server omits costs", () => {
    const el = panel([log("success")], null);
    expect(el.textContent).toContain("run complete");
    expect(el.textContent).toContain("1/1 nodes succeeded");
    expect(el.textContent).not.toContain("charged");
    expect(el.textContent).not.toContain("$");
  });
});

describe("console payment receipts", () => {
  const inboundHash = `0x${"a".repeat(64)}`;
  const outboundHash = `0x${"b".repeat(64)}`;
  const explorer = "https://explorer.testnet.whitechain.io/tx/";

  it("shows full hashes and distinct explorer links for both wallet legs", () => {
    const el = panel([{
      ...log("success"),
      nodeType: "tool402",
      output: {
        nodeName: "Tiny x402 search",
        txId: inboundHash,
        explorerURL: explorer + inboundHash,
        outboundTxId: outboundHash,
        outboundExplorerURL: explorer + outboundHash,
        amount: "0.010000",
        response: { results: [] },
      },
    }], null);

    const links = el.querySelectorAll("a");
    expect(links).toHaveLength(2);
    expect(el.textContent).toContain("Wallet 1 → Wallet 2");
    expect(el.textContent).toContain("Wallet 2 → x402");
    expect(links[0].textContent).toBe(inboundHash);
    expect(links[0].getAttribute("href")).toBe(explorer + inboundHash);
    expect(links[0].getAttribute("title")).toBe(explorer + inboundHash);
    expect(links[1].textContent).toBe(outboundHash);
    expect(links[1].getAttribute("href")).toBe(explorer + outboundHash);
    expect(links[1].getAttribute("title")).toBe(explorer + outboundHash);
    expect(el.textContent).toContain('"results": []');
  });

  it("does not reuse the funding hash for a missing outbound settlement", () => {
    const el = panel([{
      ...log("success"),
      nodeType: "tool402",
      output: {
        txId: inboundHash,
        explorerURL: explorer + inboundHash,
        isFundingReceipt: true,
        amount: "1.510000",
      },
    }], null);

    const links = el.querySelectorAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].textContent).toBe(inboundHash);
    expect(el.textContent).toContain("Wallet 1 → Wallet 2");
    expect(el.textContent).not.toContain("Wallet 2 → x402");
  });
});
