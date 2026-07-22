"use client";
import { useState } from "react";
import { RazorpayCheckoutButton } from "@/components/billing/RazorpayCheckoutButton";
import { CheckoutModal } from "@/components/checkout/CheckoutModal";

const PRESETS_INR_PAISE = [10000, 50000, 100000, 200000]; // ₹100, ₹500, ₹1000, ₹2000

export default function BillingPage() {
  const [amountPaise, setAmountPaise] = useState(PRESETS_INR_PAISE[1]);
  const [customINR, setCustomINR] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [checkoutOpen, setCheckoutOpen] = useState(false);

  const effectiveAmountPaise = customINR
    ? Math.round(parseFloat(customINR) * 100)
    : amountPaise;

  return (
    <div style={{ maxWidth: 480, margin: "48px auto", padding: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 16 }}>
        Add credits
      </h1>

      <div style={{ display: "flex", gap: 8, marginBottom: 16 }}>
        {PRESETS_INR_PAISE.map((p) => (
          <button
            key={p}
            onClick={() => {
              setAmountPaise(p);
              setCustomINR("");
            }}
            style={{
              height: 32,
              padding: "0 12px",
              borderRadius: 6,
              border:
                amountPaise === p && !customINR
                  ? "1px solid var(--accent)"
                  : "1px solid var(--border)",
              background: "transparent",
              cursor: "pointer",
            }}
          >
            ₹{p / 100}
          </button>
        ))}
      </div>

      <input
        type="number"
        placeholder="Custom amount (INR)"
        value={customINR}
        onChange={(e) => setCustomINR(e.target.value)}
        style={{
          width: "100%",
          height: 36,
          padding: "0 12px",
          borderRadius: 6,
          border: "1px solid var(--border)",
          marginBottom: 16,
        }}
      />

      <div
        style={{
          display: "flex",
          gap: 12,
          alignItems: "center",
          marginTop: 24,
        }}
      >
        {effectiveAmountPaise >= 100 ? (
          <RazorpayCheckoutButton
            amountINRPaise={effectiveAmountPaise}
            onSuccess={(credited) =>
              setMessage(`Credited $${(credited / 1e6).toFixed(2)}`)
            }
            onError={(err) => setMessage(`Error: ${err}`)}
          />
        ) : (
          <p style={{ color: "var(--danger)", fontSize: 13 }}>
            Minimum amount is ₹1
          </p>
        )}

        <button
          type="button"
          onClick={() => setCheckoutOpen(true)}
          style={{
            height: 36,
            padding: "0 16px",
            borderRadius: "var(--r-2)",
            border: "1px solid var(--border-strong)",
            background: "transparent",
            color: "var(--fg-muted)",
            fontSize: 13,
            fontWeight: 500,
            cursor: "pointer",
          }}
        >
          Open checkout
        </button>
      </div>

      {message && <p style={{ marginTop: 16, fontSize: 13 }}>{message}</p>}

      {checkoutOpen && (
        <CheckoutModal open onClose={() => setCheckoutOpen(false)} />
      )}
    </div>
  );
}
