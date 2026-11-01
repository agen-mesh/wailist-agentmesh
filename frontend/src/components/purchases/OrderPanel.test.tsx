import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OrderPanel, type OrderPanelProps } from "./OrderPanel";
import { config, order, PAYER, TEST_NOW } from "./test-fixtures";

afterEach(cleanup);

function props(overrides: Partial<OrderPanelProps> = {}): OrderPanelProps {
  return { order: order(), config, account: PAYER, busy: false, stale: false, now: TEST_NOW,
    onIdentity: vi.fn(), onDecision: vi.fn(), onSign: vi.fn(), onReconcile: vi.fn(), onCancel: vi.fn(), onRefresh: vi.fn(), ...overrides };
}

describe("exact purchase controls", () => {
  it("keeps fresh verification separate from an explicit approve click", () => {
    const data = props({ order: order({ state: "awaiting_approval", approvedAt: undefined, canApprove: true, canAuthorize: false }) });
    render(<OrderPanel {...data} />);
    expect(data.onDecision).not.toHaveBeenCalled();
    expect(data.onSign).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Approve 0.01 USDC" }));
    expect(data.onDecision).toHaveBeenCalledExactlyOnceWith("approve");
    expect(data.onSign).not.toHaveBeenCalled();
  });

  it("disables stale or expired order actions while allowing status refresh", () => {
    const data = props({ stale: true });
    render(<OrderPanel {...data} />);
    expect((screen.getByRole("button", { name: "Sign exact payment" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Refresh order" }));
    expect(data.onRefresh).toHaveBeenCalledOnce();
    expect(data.onSign).not.toHaveBeenCalled();
  });

  it("never offers cancellation or another signature after release even with inconsistent action flags", () => {
    render(<OrderPanel {...props({ order: order({ state: "payment_unknown", signatureReleasedAt: "2026-11-05T12:00:00Z", canReconcile: true }) })} />);
    expect(screen.queryByRole("button", { name: "Cancel unsigned order" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Sign exact payment" })).toBeNull();
    expect(screen.getByRole("button", { name: "Reconcile original order" })).toBeTruthy();
    expect(screen.queryByText("Service result")).toBeNull();
  });

  it("shows the non-refund rule before approval and before signing", () => {
    const policy = "A confirmed USDC payment is sent directly to the payee. If the service fails after that payment, it is not refunded.";
    const approve = props({ order: order({ state: "awaiting_approval", approvedAt: undefined, canApprove: true, canAuthorize: false, refundPolicy: policy }) });
    const view = render(<OrderPanel {...approve} />);
    const notice = screen.getByText(policy);
    const approveButton = screen.getByRole("button", { name: "Approve 0.01 USDC" });
    expect(approveButton.compareDocumentPosition(notice) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    view.unmount();
    render(<OrderPanel {...props({ order: order({ refundPolicy: policy }) })} />);
    const sign = screen.getByRole("button", { name: "Sign exact payment" });
    expect(sign.compareDocumentPosition(screen.getByText(policy)) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
  });

  it("does not link a simulated receipt to a live explorer", () => {
    const txHash = `0x${"7".repeat(64)}`;
    render(<OrderPanel {...props({ order: order({ state: "completed", result: { summary: "Result" }, txHash,
      paymentEvent: { txHash, chainId: 5042002, logIndex: "0", blockHash: `0x${"8".repeat(64)}`, blockNumber: "123" },
      signatureReleasedAt: "2026-11-05T12:00:00Z", canAuthorize: false, canCancel: false }) })} />);
    expect(screen.getByText("Verified payment receipt")).toBeTruthy();
    expect(screen.getByText(/Simulated receipt/)).toBeTruthy();
    expect(screen.queryByRole("link", { name: "View transaction on Arc explorer" })).toBeNull();
    expect(screen.getByText(/"summary": "Result"/)).toBeTruthy();
  });
});
