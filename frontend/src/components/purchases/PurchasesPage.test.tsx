import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PurchaseOrder } from "@/lib/purchase";
import { authorization, config, offer, order, PAYER, TEST_NOW } from "./test-fixtures";

const api = vi.hoisted(() => ({
  config: vi.fn(), offers: vi.fn(), wallet: vi.fn(), allowance: vi.fn(), getOrder: vi.fn(),
  beginIdentity: vi.fn(), decideOrder: vi.fn(), prepareAuthorization: vi.fn(), submitAuthorization: vi.fn(),
  reconcileOrder: vi.fn(), cancelOrder: vi.fn(),
}));
vi.mock("@/lib/purchase", () => ({ purchaseAPI: api }));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => ({ loading: false, signedIn: true, user: { id: "account-a" }, offline: false, retry: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => <header>AgentMesh</header> }));
vi.mock("./ProviderOfferForm", () => ({ ProviderOfferForm: () => null }));
import { PurchasesPage } from "./PurchasesPage";

let stored: PurchaseOrder;
let walletRequest: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks(); vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(TEST_NOW);
  sessionStorage.clear(); window.history.replaceState(null, "", "/purchases?order=order-1&success=true");
  stored = order({ state: "awaiting_approval", approvedAt: undefined, canApprove: true, canAuthorize: false });
  api.config.mockResolvedValue(config); api.offers.mockResolvedValue({ offers: [offer()] });
  api.wallet.mockResolvedValue({ address: PAYER, verifiedAt: "2026-11-05T11:00:00Z" });
  api.allowance.mockResolvedValue({ wallet: PAYER, chainId: config.chainId, asset: config.asset, limitMicros: "1000000", reservedMicros: "0", consumedMicros: "0", availableMicros: "1000000" });
  api.getOrder.mockImplementation(async () => stored);
  api.decideOrder.mockImplementation(async (_id, _hash, decision) => {
    stored = decision === "approve" ? order() : order({ state: "denied", approvedAt: undefined, canAuthorize: false, canCancel: false });
    return stored;
  });
  api.prepareAuthorization.mockImplementation(async () => { const result = authorization(); stored = result.order; return result; });
  walletRequest = vi.fn(async ({ method }: { method: string }) => {
    if (method === "eth_accounts") return [PAYER];
    if (method === "eth_chainId") return "0x4cef52";
    if (method === "eth_signTypedData_v4") return `0x${"a".repeat(130)}`;
    throw new Error("Unexpected wallet request");
  });
  Object.defineProperty(window, "ethereum", { configurable: true, value: { request: walletRequest } });
});

afterEach(() => { cleanup(); vi.useRealTimers(); Reflect.deleteProperty(window, "ethereum"); });

describe("purchase action sequence", () => {
  it("does not approve or sign because a World return URL says success", async () => {
    render(<PurchasesPage />);
    await screen.findByRole("button", { name: "Approve 0.01 USDC" });
    expect(api.decideOrder).not.toHaveBeenCalled();
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(api.submitAuthorization).not.toHaveBeenCalled();
    expect(walletRequest.mock.calls.some(([args]) => args.method === "eth_signTypedData_v4")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Approve 0.01 USDC" }));
    await waitFor(() => expect(api.decideOrder).toHaveBeenCalledExactlyOnceWith("order-1", order().intentHash, "approve"));
    await waitFor(() => expect((screen.getByRole("button", { name: "Sign exact payment" }) as HTMLButtonElement).disabled).toBe(false));
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
  });

  it("reconciles a lost submission response without obtaining another authorization", async () => {
    stored = order();
    api.submitAuthorization.mockImplementation(async () => {
      stored = { ...authorization().order, state: "payment_unknown" };
      throw new Error("Submission response lost");
    });
    api.reconcileOrder.mockImplementation(async () => { stored = { ...stored, state: "completed", result: { summary: "Done" }, canReconcile: false }; return stored; });
    const view = render(<PurchasesPage />);
    await waitFor(() => expect((screen.getByRole("button", { name: "Sign exact payment" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Sign exact payment" }));
    await screen.findByText("Payment outcome unresolved");
    expect(screen.queryByText("Service result")).toBeNull();
    expect(api.prepareAuthorization).toHaveBeenCalledOnce();
    expect(api.submitAuthorization).toHaveBeenCalledOnce();
    view.unmount();
    render(<PurchasesPage />);
    await screen.findByText("Payment outcome unresolved");
    expect(screen.queryByRole("button", { name: "Sign exact payment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Cancel unsigned order" })).toBeNull();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Reconcile original order" })));
    await screen.findByText("Service complete");
    expect(api.prepareAuthorization).toHaveBeenCalledOnce();
    expect(api.submitAuthorization).toHaveBeenCalledOnce();
    expect(api.reconcileOrder).toHaveBeenCalledExactlyOnceWith("order-1");
  });

  it("keeps configuration failure visible instead of providing successful fallback data", async () => {
    api.config.mockRejectedValue(new Error("offline")); api.offers.mockRejectedValue(new Error("offline"));
    render(<PurchasesPage />);
    await screen.findByText("Purchase services are unavailable. No payment actions can start.");
    expect((screen.getByRole("button", { name: "Approve 0.01 USDC" }) as HTMLButtonElement).disabled).toBe(true);
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(screen.queryByText("Service complete")).toBeNull();
  });
});
