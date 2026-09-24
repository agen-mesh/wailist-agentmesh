import { describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

// @cashfreepayments/cashfree-js injects Cashfree's script the moment it is
// imported, not when load() is called. The mock's factory stands in for that
// import-time side effect, so counting its runs says when the SDK is fetched.
const sdk = vi.hoisted(() => ({ imported: 0 }));
vi.mock("@cashfreepayments/cashfree-js", () => {
  sdk.imported += 1;
  return { load: vi.fn(async () => ({ checkout: vi.fn() })) };
});
vi.mock("@/lib/api", () => ({ payments: {} }));

import { useCashfreeCheckout } from "./useCashfreeCheckout";

describe("useCashfreeCheckout", () => {
  // The Credits page imports the checkout modal, so a static import fetched
  // the payment SDK on every visit -- including in the Android app, which
  // never takes payment and whose CSP refuses the script.
  it("fetches the payment SDK only once a checkout mounts", async () => {
    expect(sdk.imported).toBe(0);

    const { result } = renderHook(() =>
      useCashfreeCheckout({ onSuccess: vi.fn(), onError: vi.fn() }),
    );

    await waitFor(() => expect(sdk.imported).toBe(1));
    await waitFor(() => expect(result.current.ready).toBe(true));
  });
});
