"use client";
import { useState } from "react";
import { Pill } from "@/components/ui";
import type { PaymentMethod } from "./types";
import { PAYMENT_PROVIDERS } from "./paymentProviders";
import { useRazorpayCheckout } from "./useRazorpayCheckout";

type PayStatus = "idle" | "processing" | "success";

const PANEL_CSS = `
.checkout-pay { transition: background 0.18s var(--ease), transform 0.12s var(--ease); }
.checkout-pay:not(:disabled):active { transform: scale(0.985); }
.checkout-provider { transition: border-color 0.15s var(--ease), background 0.15s var(--ease); }
@media (prefers-reduced-motion: reduce) {
  .checkout-pay, .checkout-provider { transition: none; }
}
`;

// Right-hand payment column: pick a provider, then pay. Razorpay runs the real
// hosted-checkout flow (order → pay → server-side verify); NOWPayments is wired
// as a stub until its backend lands. PayPal and Stripe render disabled. The
// provider hosts card entry, so there is no in-app card form here.
export function PaymentInfoPanel({
  method,
  onMethodChange,
  amountINR,
  payable,
  onPaid,
}: {
  method: PaymentMethod;
  onMethodChange: (method: PaymentMethod) => void;
  amountINR: number;
  payable: boolean;
  onPaid: () => void;
}) {
  const [status, setStatus] = useState<PayStatus>("idle");
  const [error, setError] = useState<string | null>(null);

  const finish = () => {
    setStatus("success");
    onPaid();
  };

  const razorpay = useRazorpayCheckout({
    onSuccess: () => finish(),
    onError: (msg) => {
      setError(msg);
      setStatus("idle");
    },
    onDismiss: () => setStatus("idle"),
  });

  const selected = PAYMENT_PROVIDERS.find((p) => p.id === method);
  const busy = status === "processing" || razorpay.loading;
  const isSuccess = status === "success";
  const canPay = !!selected?.enabled && payable && !busy && !isSuccess;

  const handlePay = () => {
    if (!canPay) return;
    setError(null);
    if (method === "razorpay") {
      // useRazorpayCheckout owns loading/dismiss; leave status idle so the
      // button reflects the hook's state, not a stuck "processing".
      razorpay.pay(Math.round(amountINR * 100));
    } else if (method === "nowpayments") {
      // TODO: NOWPayments has no backend yet — simulate the hosted-invoice
      // round-trip so the credits flow is demoable frontend-only.
      setStatus("processing");
      window.setTimeout(finish, 900);
    }
  };

  const buttonLabel = isSuccess
    ? "✓ Payment successful"
    : busy
      ? "Processing…"
      : payable
        ? `Pay ₹${amountINR.toFixed(2)}`
        : "Add an amount to continue";

  const trust =
    method === "nowpayments"
      ? "Settled on-chain via NOWPayments"
      : "Secured by Razorpay · details are encrypted";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
      <style>{PANEL_CSS}</style>
      <div style={{ fontSize: 16, fontWeight: 600, color: "var(--fg)" }}>
        Payment method
      </div>

      {/* Provider selector */}
      <div
        role="radiogroup"
        aria-label="Payment method"
        style={{ display: "flex", flexDirection: "column", gap: 10 }}
      >
        {PAYMENT_PROVIDERS.map((p) => {
          const active = method === p.id;
          const disabled = !p.enabled;
          return (
            <button
              key={p.id}
              type="button"
              className="checkout-provider"
              role="radio"
              aria-checked={active}
              aria-disabled={disabled}
              disabled={disabled}
              onClick={() => !disabled && onMethodChange(p.id)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                width: "100%",
                textAlign: "left",
                padding: "12px 14px",
                borderRadius: "var(--r-2)",
                border: `1px solid ${active ? "var(--accent-line)" : "var(--border)"}`,
                background: active ? "var(--accent-soft)" : "var(--bg)",
                color: disabled ? "var(--fg-dim)" : "var(--fg)",
                cursor: disabled ? "not-allowed" : "pointer",
                opacity: disabled ? 0.55 : 1,
                fontFamily: "var(--font-sans)",
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 16,
                  height: 16,
                  borderRadius: 999,
                  flexShrink: 0,
                  border: `1px solid ${active ? "var(--accent)" : "var(--border-strong)"}`,
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                }}
              >
                {active && (
                  <span
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: 999,
                      background: "var(--accent)",
                    }}
                  />
                )}
              </span>
              <span
                style={{ display: "flex", flexDirection: "column", gap: 2 }}
              >
                <span style={{ fontSize: 13, fontWeight: 500 }}>{p.label}</span>
                <span style={{ fontSize: 11, color: "var(--fg-dim)" }}>
                  {p.sublabel}
                </span>
              </span>
              {disabled && (
                <span style={{ marginLeft: "auto" }}>
                  <Pill>Soon</Pill>
                </span>
              )}
            </button>
          );
        })}
      </div>

      <div style={{ marginTop: "auto" }}>
        {/* Trust signal — reassurance right at the point of payment. */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: 6,
            marginBottom: 12,
            color: "var(--fg-dim)",
            fontSize: 12,
          }}
        >
          <svg
            width="12"
            height="12"
            viewBox="0 0 16 16"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="3"
              y="7"
              width="10"
              height="7"
              rx="1.5"
              stroke="currentColor"
              strokeWidth="1.3"
            />
            <path
              d="M5 7V5a3 3 0 0 1 6 0v2"
              stroke="currentColor"
              strokeWidth="1.3"
            />
          </svg>
          {trust}
        </div>

        <button
          type="button"
          className="checkout-pay"
          onClick={handlePay}
          disabled={!canPay}
          style={{
            height: 44,
            width: "100%",
            background: isSuccess ? "var(--bg-elev-3)" : "var(--accent)",
            border: "1px solid var(--accent-line)",
            borderRadius: "var(--r-2)",
            color: isSuccess ? "var(--accent)" : "var(--accent-fg)",
            fontSize: 14,
            fontWeight: 600,
            cursor: canPay ? "pointer" : "default",
            opacity: !canPay && !isSuccess ? 0.5 : 1,
            fontFamily: "var(--font-sans)",
          }}
        >
          {buttonLabel}
        </button>

        {error && (
          <p
            style={{
              margin: "10px 0 0",
              fontSize: 12,
              color: "var(--danger)",
              textAlign: "center",
            }}
          >
            {error}
          </p>
        )}

        {!error && !busy && !isSuccess && (
          <p
            style={{
              margin: "10px 0 0",
              fontSize: 12,
              color: "var(--fg-dim)",
              textAlign: "center",
            }}
          >
            {!payable
              ? "Your cart is empty."
              : `You'll be redirected to ${selected?.label ?? "the provider"} to complete payment.`}
          </p>
        )}
      </div>
    </div>
  );
}
