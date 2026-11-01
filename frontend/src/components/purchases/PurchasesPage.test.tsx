import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PurchaseAllowance, PurchaseOrder } from "@/lib/purchase";
import { authorization, config, offer, order, PAYER, TEST_NOW } from "./test-fixtures";

const api = vi.hoisted(() => ({
  config: vi.fn(), offers: vi.fn(), wallet: vi.fn(), allowance: vi.fn(), getOrder: vi.fn(),
  beginIdentity: vi.fn(), decideOrder: vi.fn(), prepareAuthorization: vi.fn(), submitAuthorization: vi.fn(),
  reconcileOrder: vi.fn(), cancelOrder: vi.fn(), createOrder: vi.fn(),
}));
const providerForm = vi.hoisted(() => vi.fn());
const session = vi.hoisted(() => ({ accountId: "account-a" }));
vi.mock("@/lib/purchase", () => ({ purchaseAPI: api }));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => ({ loading: false, signedIn: true, user: { id: session.accountId }, offline: false, retry: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => <header>AgentMesh</header> }));
vi.mock("./ProviderOfferForm", () => ({ ProviderOfferForm: (props: { disabled: boolean }) => { providerForm(props); return null; } }));
import { PurchasesPage } from "./PurchasesPage";

let stored: PurchaseOrder;
let budget: PurchaseAllowance;
let walletRequest: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(TEST_NOW);
  session.accountId = "account-a";
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  sessionStorage.clear(); window.history.replaceState(null, "", "/purchases?order=order-1&success=true");
  stored = order({ state: "awaiting_approval", approvedAt: undefined, canApprove: true, canAuthorize: false });
  api.config.mockResolvedValue(config); api.offers.mockResolvedValue({ offers: [offer()] });
  api.wallet.mockResolvedValue({ address: PAYER, verifiedAt: "2026-11-05T11:00:00Z" });
  budget = { wallet: PAYER, chainId: config.chainId, asset: config.asset, limitMicros: "50000", reservedMicros: "0", consumedMicros: "0", availableMicros: "50000" };
  api.allowance.mockImplementation(async () => ({ ...budget }));
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

afterEach(() => { cleanup(); vi.useRealTimers(); Reflect.deleteProperty(window, "ethereum"); Reflect.deleteProperty(document, "visibilityState"); });

describe("purchase action sequence", () => {
  it.each([7738577, 1, undefined])("blocks provider publishing when catalog chain %s differs from the browser", async (arkivChainId) => {
    api.config.mockResolvedValue({ ...config, arkivChainId });
    await act(async () => { render(<PurchasesPage />); });
    expect(screen.getByText("Provider publishing is unavailable because the browser and catalog networks differ.")).toBeTruthy();
    expect(providerForm.mock.lastCall?.[0].disabled).toBe(true);
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(api.submitAuthorization).not.toHaveBeenCalled();
  });

  it("shows the Rabdi migration boundary and enables a matching provider catalog", async () => {
    await act(async () => { render(<PurchasesPage />); });
    expect(screen.getByText(/Tiramisu listings do not carry over to Rabdi/)).toBeTruthy();
    expect(providerForm.mock.lastCall?.[0].disabled).toBe(false);
    expect(screen.queryByText("Provider publishing is unavailable because the browser and catalog networks differ.")).toBeNull();
  });

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
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Reconcile original order" })); });
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

  it("keeps Arkiv discovery, inspection and provider publishing available without World checkout", async () => {
    window.history.replaceState(null, "", "/purchases");
    api.config.mockResolvedValue({ ...config, enabled: false, worldAvailable: false, arkivAvailable: true, blockedReasons: ["World is not configured"] });
    api.wallet.mockResolvedValue(null); api.allowance.mockRejectedValue(new Error("Wallet not linked"));
    render(<PurchasesPage />);
    await screen.findByText("Checkout is unavailable.");
    const inspect = screen.getByRole("button", { name: "Inspect service and price" });
    expect((inspect as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Refresh discovery" }) as HTMLButtonElement).disabled).toBe(false);
    expect(providerForm.mock.lastCall?.[0].disabled).toBe(false);
    fireEvent.click(inspect);
    expect(screen.getByText("Required service input schema")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Create exact order" }) as HTMLButtonElement).disabled).toBe(true);
    expect(api.beginIdentity).not.toHaveBeenCalled();
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
  });
});

describe("authoritative spending-limit refresh", () => {
  it("shows consumed funds after payment completes without a reload", async () => {
    stored = order();
    api.submitAuthorization.mockImplementation(async () => {
      budget = { ...budget, consumedMicros: "10000", availableMicros: "40000" };
      stored = { ...authorization().order, state: "completed", result: "Service delivered", canReconcile: false };
      return stored;
    });
    render(<PurchasesPage />);
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    fireEvent.click(screen.getByRole("button", { name: "Sign exact payment" }));
    await screen.findByText("Service complete");
    await screen.findByText("0.04 available · 0 reserved · 0.01 spent");
    expect(api.submitAuthorization).toHaveBeenCalledOnce();
  });

  it("refreshes reserved funds when background order polling observes release", async () => {
    stored = order();
    render(<PurchasesPage />);
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    stored = authorization().order;
    budget = { ...budget, reservedMicros: "10000", availableMicros: "40000" };
    fireEvent(document, new Event("visibilitychange"));
    await screen.findByText("Authorization prepared");
    await screen.findByText("0.04 available · 0.01 reserved · 0 spent");
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(api.submitAuthorization).not.toHaveBeenCalled();
  });

  it("refreshes released reservations after explicit reconciliation", async () => {
    stored = { ...authorization().order, state: "payment_unknown" };
    budget = { ...budget, reservedMicros: "10000", availableMicros: "40000" };
    api.reconcileOrder.mockImplementation(async () => {
      stored = { ...stored, state: "expired", canReconcile: false };
      budget = { ...budget, reservedMicros: "0", availableMicros: "50000" };
      return stored;
    });
    render(<PurchasesPage />);
    await screen.findByText("0.04 available · 0.01 reserved · 0 spent");
    fireEvent.click(screen.getByRole("button", { name: "Reconcile original order" }));
    await screen.findByText("Order expired");
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
  });

  it("reads the reservation even when preparation and the order refresh lose their responses", async () => {
    stored = order();
    api.prepareAuthorization.mockImplementation(async () => {
      budget = { ...budget, reservedMicros: "10000", availableMicros: "40000" };
      api.getOrder.mockRejectedValue(new Error("Order response lost"));
      throw new Error("Preparation response lost");
    });
    render(<PurchasesPage />);
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    fireEvent.click(screen.getByRole("button", { name: "Sign exact payment" }));
    await screen.findByText("Preparation response lost");
    await screen.findByText("0.04 available · 0.01 reserved · 0 spent");
    expect(api.submitAuthorization).not.toHaveBeenCalled();
  });

  it("removes stale figures when the ledger read fails after a state change", async () => {
    stored = order();
    await act(async () => { render(<PurchasesPage />); });
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    api.allowance.mockRejectedValue(new Error("Ledger unavailable"));
    stored = { ...authorization().order, state: "payment_unknown" };
    fireEvent(document, new Event("visibilitychange"));
    await screen.findByText(/Your spending limit could not be refreshed/);
    expect(screen.queryByText("0.05 available · 0 reserved · 0 spent")).toBeNull();
    expect(screen.getByText("Payment outcome unresolved")).toBeTruthy();
  });

  it("does not replace a newer ledger response with an older delayed read", async () => {
    stored = order();
    render(<PurchasesPage />);
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    let finishOld!: (value: PurchaseAllowance) => void;
    api.allowance.mockImplementationOnce(() => new Promise<PurchaseAllowance>((resolve) => { finishOld = resolve; }));
    stored = authorization().order;
    fireEvent(document, new Event("visibilitychange"));
    await waitFor(() => expect(finishOld).toBeTypeOf("function"));
    stored = { ...stored, state: "completed", result: "Service delivered", canReconcile: false };
    budget = { ...budget, consumedMicros: "10000", availableMicros: "40000" };
    fireEvent(document, new Event("visibilitychange"));
    await screen.findByText("0.04 available · 0 reserved · 0.01 spent");
    await act(async () => { finishOld({ ...budget, consumedMicros: "0", reservedMicros: "10000" }); });
    expect(screen.getByText("0.04 available · 0 reserved · 0.01 spent")).toBeTruthy();
  });

  it("ignores the previous account's delayed ledger response", async () => {
    stored = order();
    const view = render(<PurchasesPage />);
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    let finishOld!: (value: PurchaseAllowance) => void;
    api.allowance.mockImplementationOnce(() => new Promise<PurchaseAllowance>((resolve) => { finishOld = resolve; }));
    stored = authorization().order;
    fireEvent(document, new Event("visibilitychange"));
    await waitFor(() => expect(finishOld).toBeTypeOf("function"));
    session.accountId = "account-b";
    budget = { ...budget, limitMicros: "200000", availableMicros: "200000" };
    api.getOrder.mockRejectedValue(new Error("Order belongs to another account"));
    view.rerender(<PurchasesPage />);
    await screen.findByText("0.2 available · 0 reserved · 0 spent");
    await act(async () => { finishOld({ ...budget, limitMicros: "50000", availableMicros: "40000", reservedMicros: "10000" }); });
    expect(screen.getByText("0.2 available · 0 reserved · 0 spent")).toBeTruthy();
  });
});

describe("new purchases after confirmed unused expiry", () => {
  function allowNewOrder() {
    api.createOrder.mockImplementation(async () => {
      stored = order({ id: "order-2", intentHash: `0x${"6".repeat(64)}`, state: "awaiting_identity",
        identityVerifiedAt: undefined, approvedAt: undefined, canApprove: false, canAuthorize: false });
      return stored;
    });
  }

  async function createSelectedOrder() {
    const create = screen.getByRole("button", { name: "Create exact order" });
    await waitFor(() => expect((create as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(create);
    await screen.findByText("Fresh verification needed");
    expect(api.createOrder).toHaveBeenCalledExactlyOnceWith({
      offerKey: offer().key, request: {}, maxMicros: "10000", clientRequestId: expect.any(String),
    });
    expect(new URL(window.location.href).searchParams.get("order")).toBe("order-2");
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(api.submitAuthorization).not.toHaveBeenCalled();
  }

  it("allows a new order after restoring server-confirmed expired-unused state", async () => {
    stored = { ...authorization().order, state: "expired", deadline: "2026-11-05T11:59:59Z", canReconcile: false,
      reason: "Authorization expired unused at a confirmed chain block" };
    allowNewOrder();
    const first = render(<PurchasesPage />);
    await screen.findByText("Order expired");
    first.unmount();
    render(<PurchasesPage />);
    await screen.findByText("Order expired");
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    expect(stored.signatureReleasedAt).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Inspect service and price" }));
    await createSelectedOrder();
  });

  it("allows a new order once reconciliation confirms expiry and releases the reservation", async () => {
    stored = { ...authorization().order, state: "payment_unknown", deadline: "2026-11-05T11:59:59Z" };
    budget = { ...budget, reservedMicros: "10000", availableMicros: "40000" };
    api.reconcileOrder.mockImplementation(async () => {
      stored = { ...stored, state: "expired", canReconcile: false,
        reason: "Authorization expired unused at a confirmed chain block" };
      budget = { ...budget, reservedMicros: "0", availableMicros: "50000" };
      return stored;
    });
    allowNewOrder();
    render(<PurchasesPage />);
    await screen.findByText("0.04 available · 0.01 reserved · 0 spent");
    fireEvent.click(screen.getByRole("button", { name: "Inspect service and price" }));
    expect((screen.getByRole("button", { name: "Create exact order" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Reconcile original order" }));
    await screen.findByText("Order expired");
    await screen.findByText("0.05 available · 0 reserved · 0 spent");
    expect(stored.signatureReleasedAt).toBeTruthy();
    await createSelectedOrder();
  });

  it.each([
    { label: "released authorization after its local deadline", state: "authorization_released", deadline: "2026-11-05T11:59:59Z" },
    { label: "pending submission after its local deadline", state: "submitting", deadline: "2026-11-05T11:59:59Z" },
    { label: "unknown payment before its deadline", state: "payment_unknown", deadline: "2026-11-05T12:10:00Z" },
    { label: "unknown payment after its local deadline", state: "payment_unknown", deadline: "2026-11-05T11:59:59Z" },
  ] as const)("keeps a new purchase blocked for $label", async ({ state, deadline }) => {
    stored = { ...authorization().order, state, deadline };
    budget = { ...budget, reservedMicros: "10000", availableMicros: "40000" };
    render(<PurchasesPage />);
    await screen.findByText("0.04 available · 0.01 reserved · 0 spent");
    fireEvent.click(screen.getByRole("button", { name: "Inspect service and price" }));
    const create = screen.getByRole("button", { name: "Create exact order" });
    expect((create as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(create);
    expect(api.createOrder).not.toHaveBeenCalled();
    expect(screen.queryByText("Order expired")).toBeNull();
    expect(api.prepareAuthorization).not.toHaveBeenCalled();
    expect(api.submitAuthorization).not.toHaveBeenCalled();
  });
});
