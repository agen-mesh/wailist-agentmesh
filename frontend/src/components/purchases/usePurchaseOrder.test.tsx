import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PurchaseOrder } from "@/lib/purchase";
import { storedOrderKey } from "./format";
import { order } from "./test-fixtures";

const api = vi.hoisted(() => ({ getOrder: vi.fn() }));
vi.mock("@/lib/purchase", () => ({ purchaseAPI: api }));
import { usePurchaseOrder } from "./usePurchaseOrder";

beforeEach(() => { sessionStorage.clear(); window.history.replaceState(null, "", "/purchases"); vi.clearAllMocks(); });
afterEach(cleanup);

describe("authoritative order recovery", () => {
  it("ignores callback success flags and loads the order from the server", async () => {
    window.history.replaceState(null, "", "/purchases?order=order-1&success=true&world=verified");
    api.getOrder.mockResolvedValue(order({ state: "awaiting_identity", identityVerifiedAt: undefined, approvedAt: undefined, canApprove: false, canAuthorize: false }));
    const { result } = renderHook(() => usePurchaseOrder("account-a", false));
    await waitFor(() => expect(result.current.order?.state).toBe("awaiting_identity"));
    expect(result.current.order?.canApprove).toBe(false);
    expect(result.current.order?.canAuthorize).toBe(false);
    expect(api.getOrder).toHaveBeenCalledExactlyOnceWith("order-1");
    expect(window.location.search).toBe("?order=order-1");
  });

  it("does not restore another account's cached order", async () => {
    sessionStorage.setItem(storedOrderKey("account-a"), "order-other");
    const { result } = renderHook(() => usePurchaseOrder("account-b", false));
    await act(async () => { await Promise.resolve(); });
    expect(api.getOrder).not.toHaveBeenCalled();
    expect(result.current.order).toBeNull();
  });

  it("persists only the order ID and restores release status from the backend", async () => {
    const released = order({ state: "authorization_released", signatureReleasedAt: "2026-11-05T12:00:00Z", canAuthorize: false, canCancel: false, canReconcile: true });
    const first = renderHook(() => usePurchaseOrder("account-a", false));
    await act(async () => { await Promise.resolve(); });
    act(() => first.result.current.select(released));
    expect(sessionStorage.getItem(storedOrderKey("account-a"))).toBe("order-1");
    expect(sessionStorage.length).toBe(1);
    first.unmount();
    api.getOrder.mockResolvedValue(released);
    const second = renderHook(() => usePurchaseOrder("account-a", false));
    await waitFor(() => expect(second.result.current.order?.state).toBe("authorization_released"));
    expect(second.result.current.order?.canCancel).toBe(false);
    expect(second.result.current.order?.canAuthorize).toBe(false);
  });

  it("does not replace a newly selected order with an older delayed response", async () => {
    window.history.replaceState(null, "", "/purchases?order=order-old");
    let resolve!: (order: PurchaseOrder) => void;
    api.getOrder.mockReturnValue(new Promise<PurchaseOrder>((done) => { resolve = done; }));
    const { result } = renderHook(() => usePurchaseOrder("account-a", false));
    await waitFor(() => expect(api.getOrder).toHaveBeenCalledWith("order-old"));
    act(() => result.current.select(order({ id: "order-new" })));
    await act(async () => resolve(order({ id: "order-old" })));
    expect(result.current.order?.id).toBe("order-new");
    expect(sessionStorage.getItem(storedOrderKey("account-a"))).toBe("order-new");
  });

  it("keeps the last order stale when refresh fails instead of inventing a successful result", async () => {
    window.history.replaceState(null, "", "/purchases?order=order-1");
    api.getOrder.mockResolvedValueOnce(order({ state: "payment_unknown", canAuthorize: false })).mockRejectedValueOnce(new Error("unavailable"));
    const { result } = renderHook(() => usePurchaseOrder("account-a", false));
    await waitFor(() => expect(result.current.order?.state).toBe("payment_unknown"));
    await act(async () => { await result.current.refresh(); });
    expect(result.current.stale).toBe(true);
    expect(result.current.order?.state).toBe("payment_unknown");
    expect(result.current.error).toContain("could not be refreshed");
  });
});
