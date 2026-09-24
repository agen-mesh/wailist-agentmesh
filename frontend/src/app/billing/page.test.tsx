import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({
  native: false,
  openExternal: vi.fn<
    (url: string, options?: { onClose?: () => void }) => Promise<void>
  >(async () => {}),
  refreshBalance: vi.fn(async () => {}),
  refreshPurchases: vi.fn(async () => {}),
}));

vi.mock("@/lib/nativeAuth", () => ({
  get IS_NATIVE() {
    return state.native;
  },
}));
vi.mock("@/lib/openExternal", () => ({
  WEB_BILLING_URL: "https://www.agent-mesh.app/billing",
  openExternal: state.openExternal,
}));
vi.mock("@/lib/credits/store", () => ({
  useCredits: () => ({
    balanceUSD: 12,
    balanceKnown: true,
    lastPurchase: undefined,
    refreshBalance: state.refreshBalance,
    refreshPurchases: state.refreshPurchases,
  }),
}));
vi.mock("@/lib/api", () => ({ credits: {} }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/components/checkout/CheckoutModal", () => ({
  CheckoutModal: () => <div>checkout dialog</div>,
}));
vi.mock("@/components/billing/PurchaseHistory", () => ({
  PurchaseHistory: ({
    onBuyAgain,
  }: {
    onBuyAgain: (amountINR: number) => void;
  }) => (
    <button type="button" onClick={() => onBuyAgain(1000)}>
      Buy again
    </button>
  ),
}));

import BillingPage from "./page";

afterEach(() => {
  cleanup();
  state.native = false;
  state.openExternal.mockClear();
  state.refreshBalance.mockClear();
  state.refreshPurchases.mockClear();
});

describe("BillingPage in the Android app", () => {
  it("tops up on the website and re-reads credits when the tab closes", () => {
    state.native = true;
    render(<BillingPage />);

    expect(screen.queryByText("Choose an amount")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: /Add credits on the website/ }),
    );

    expect(state.openExternal).toHaveBeenCalledTimes(1);
    const [url, options] = state.openExternal.mock.calls[0];
    expect(url).toBe("https://www.agent-mesh.app/billing");

    state.refreshBalance.mockClear();
    options?.onClose?.();
    expect(state.refreshBalance).toHaveBeenCalledTimes(1);
    expect(state.refreshPurchases).toHaveBeenCalledTimes(1);
  });

  it("sends Buy again to the website instead of the checkout dialog", () => {
    state.native = true;
    render(<BillingPage />);

    fireEvent.click(screen.getByRole("button", { name: "Buy again" }));

    expect(state.openExternal).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("checkout dialog")).toBeNull();
  });
});

describe("BillingPage on the web", () => {
  it("keeps the amount picker, and Buy again opens checkout", () => {
    render(<BillingPage />);

    expect(screen.getByText("Choose an amount")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /Add credits on the website/ }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Buy again" }));

    expect(screen.getByText("checkout dialog")).toBeTruthy();
    expect(state.openExternal).not.toHaveBeenCalled();
  });
});
