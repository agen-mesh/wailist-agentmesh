"use client";
import { useState } from "react";
import Script from "next/script";
import { payments } from "@/lib/api";

declare global {
  interface Window {
    Razorpay: new (options: Record<string, unknown>) => { open: () => void };
  }
}

export function RazorpayCheckoutButton({
  amountINRPaise,
  onSuccess,
  onError,
}: {
  amountINRPaise: number;
  onSuccess: (creditedUsdMicros: number) => void;
  onError: (message: string) => void;
}) {
  const [loading, setLoading] = useState(false);
  const [scriptReady, setScriptReady] = useState(false);

  async function handlePay() {
    if (!scriptReady) {
      onError("payment script still loading, try again in a moment");
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
            onSuccess(result.credited_usd_micros);
          } catch (err) {
            onError(err instanceof Error ? err.message : "verification failed");
          } finally {
            setLoading(false);
          }
        },
        modal: {
          ondismiss: () => setLoading(false),
        },
      });
      rzp.open();
    } catch (err) {
      onError(err instanceof Error ? err.message : "could not start checkout");
      setLoading(false);
    }
  }

  return (
    <>
      <Script
        src="https://checkout.razorpay.com/v1/checkout.js"
        onLoad={() => setScriptReady(true)}
        onError={() => onError("payment script failed to load")}
      />
      <button
        onClick={handlePay}
        disabled={loading || !scriptReady}
        style={{
          height: 36, padding: "0 16px", borderRadius: 8,
          border: "1px solid var(--accent-line)", background: "var(--accent)",
          color: "#fff", fontWeight: 600, fontSize: 13,
          cursor: loading || !scriptReady ? "default" : "pointer",
          opacity: loading || !scriptReady ? 0.6 : 1,
        }}
      >
        {loading ? "Opening checkout..." : !scriptReady ? "Loading payment..." : "Pay with Razorpay"}
      </button>
    </>
  );
}
