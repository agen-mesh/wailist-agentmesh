"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { payments } from "@/lib/api";

// Razorpay's hosted checkout is loaded from their CDN and attaches window.Razorpay.
declare global {
  interface Window {
    Razorpay: new (options: Record<string, unknown>) => { open: () => void };
  }
}

const SCRIPT_SRC = "https://checkout.razorpay.com/v1/checkout.js";

// Drives the real Razorpay top-up: loads checkout.js once, creates an order on
// the backend, opens the hosted modal, and verifies the signature server-side.
// Extracted from the old RazorpayCheckoutButton so the checkout modal can own
// the flow. `pay` is a no-op until the script is `ready`.
export function useRazorpayCheckout({
  onSuccess,
  onError,
  onDismiss,
}: {
  onSuccess: (creditedUsdMicros: number) => void;
  onError: (message: string) => void;
  onDismiss?: () => void;
}) {
  // Initialise from whether the script is already present so we never need a
  // synchronous setState in the effect for the already-loaded case.
  const [ready, setReady] = useState(
    () => typeof window !== "undefined" && !!window.Razorpay,
  );
  const [loading, setLoading] = useState(false);

  // Keep the latest callbacks in a ref without re-creating `pay` every render.
  // Written in an effect (not during render) so it satisfies react-hooks rules.
  const cbs = useRef({ onSuccess, onError, onDismiss });
  useEffect(() => {
    cbs.current = { onSuccess, onError, onDismiss };
  });

  useEffect(() => {
    // Nothing to load on the server or when the script is already available.
    if (typeof window === "undefined" || window.Razorpay) return;
    const onLoad = () => setReady(true);
    const onErr = () => cbs.current.onError("payment script failed to load");
    let script = document.querySelector<HTMLScriptElement>(
      `script[src="${SCRIPT_SRC}"]`,
    );
    if (!script) {
      script = document.createElement("script");
      script.src = SCRIPT_SRC;
      script.async = true;
      document.body.appendChild(script);
    }
    script.addEventListener("load", onLoad);
    script.addEventListener("error", onErr);
    return () => {
      script?.removeEventListener("load", onLoad);
      script?.removeEventListener("error", onErr);
    };
  }, []);

  const pay = useCallback(
    async (amountINRPaise: number) => {
      if (!ready) {
        cbs.current.onError("payment is still loading, try again in a moment");
        return;
      }
      setLoading(true);
      try {
        const order = await payments.createRazorpayOrder(amountINRPaise);
        const rzp = new window.Razorpay({
          key: order.key_id,
          amount: order.amount,
          currency: order.currency,
          order_id: order.order_id,
          name: "AgentMesh",
          description: "Credit top-up",
          handler: async (response: {
            razorpay_order_id: string;
            razorpay_payment_id: string;
            razorpay_signature: string;
          }) => {
            try {
              const result = await payments.verifyRazorpayPayment(response);
              cbs.current.onSuccess(result.credited_usd_micros);
            } catch (err) {
              cbs.current.onError(
                err instanceof Error ? err.message : "verification failed",
              );
            } finally {
              setLoading(false);
            }
          },
          modal: {
            ondismiss: () => {
              setLoading(false);
              cbs.current.onDismiss?.();
            },
          },
        });
        rzp.open();
      } catch (err) {
        cbs.current.onError(
          err instanceof Error ? err.message : "could not start checkout",
        );
        setLoading(false);
      }
    },
    [ready],
  );

  return { pay, ready, loading };
}
