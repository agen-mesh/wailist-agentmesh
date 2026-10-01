"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import type { CashfreeInstance } from "@cashfreepayments/cashfree-js";
import { payments } from "@/lib/api";

export function useCashfreeCheckout({
  onSuccess,
  onError,
  onDismiss,
}: {
  onSuccess: (creditedUsdMicros: number) => void;
  onError: (message: string) => void;
  onDismiss?: () => void;
}) {
  const [ready, setReady] = useState(false);
  const [loading, setLoading] = useState(false);
  const cashfreeRef = useRef<CashfreeInstance | null>(null);

  const cbs = useRef({ onSuccess, onError, onDismiss });
  useEffect(() => {
    cbs.current = { onSuccess, onError, onDismiss };
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const mode =
      process.env.NEXT_PUBLIC_CASHFREE_MODE === "sandbox"
        ? "sandbox"
        : "production";
    // Imported here, not at the top of the file: the package injects
    // Cashfree's script as soon as it is imported. The Credits page imports
    // the checkout modal, so a static import fetched the payment SDK on every
    // visit, including in the Android app, which pays on the website instead
    // and whose CSP refuses the script. Now it loads once a checkout opens.
    import("@cashfreepayments/cashfree-js")
      .then(({ load }) => load({ mode }))
      .then((cf) => {
        cashfreeRef.current = cf;
        setReady(true);
      })
      .catch(() => cbs.current.onError("payment SDK failed to load"));
  }, []);

  const pay = useCallback(
    async (amountINRPaise: number, phone: string) => {
      if (!ready || !cashfreeRef.current) {
        cbs.current.onError("payment is still loading, try again in a moment");
        return;
      }
      setLoading(true);
      try {
        const order = await payments.createCashfreeOrder(amountINRPaise, phone);

        const result = await cashfreeRef.current.checkout({
          paymentSessionId: order.payment_session_id,
          redirectTarget: "_modal",
        });

        if (result.error) {
          if (
            result.error.message?.toLowerCase().includes("cancel") ||
            result.error.type === "user_dropped"
          ) {
            cbs.current.onDismiss?.();
          } else {
            cbs.current.onError(result.error.message ?? "payment failed");
          }
          return;
        }

        // Payment may be complete -- verify server-side to get the credited amount.
        const verification = await payments.verifyCashfreePayment(
          order.order_id,
        );
        cbs.current.onSuccess(verification.credited_usd_micros);
      } catch (err) {
        cbs.current.onError(
          err instanceof Error ? err.message : "could not start checkout",
        );
      } finally {
        setLoading(false);
      }
    },
    [ready],
  );

  return { pay, ready, loading };
}
