"use client";
import { useState } from "react";
import { PurchaseHistory } from "@/components/billing/PurchaseHistory";
import { LowBalanceBanner } from "@/components/billing/LowBalanceBanner";
import { AutoRechargeSettings } from "@/components/billing/AutoRechargeSettings";
import { CheckoutModal } from "@/components/checkout/CheckoutModal";
import { useCredits } from "@/lib/credits/store";
import { bonusRate } from "@/lib/credits/fx";

const PRESETS_INR_PAISE = [10000, 50000, 100000, 200000]; // ₹100, ₹500, ₹1000, ₹2000

export default function BillingPage() {
  const [amountPaise, setAmountPaise] = useState(PRESETS_INR_PAISE[1]);
  const [customINR, setCustomINR] = useState("");
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const { lastPurchase } = useCredits();

  // Open the checkout pre-filled with a specific INR amount (used by reorder).
  const openCheckoutFor = (amountINR: number) => {
    setCustomINR(String(amountINR));
    setCheckoutOpen(true);
  };

  const effectiveAmountPaise = customINR
    ? Math.round(parseFloat(customINR) * 100)
    : amountPaise;

  // Rupees carried into the checkout modal; 0 when the amount is invalid.
  const checkoutAmountINR =
    Number.isFinite(effectiveAmountPaise) && effectiveAmountPaise >= 100
      ? effectiveAmountPaise / 100
      : 0;

  const canCheckout = checkoutAmountINR > 0;

  return (
    <div style={{ maxWidth: 480, margin: "48px auto", padding: 24 }}>
      <h1 style={{ fontSize: 20, fontWeight: 600, marginBottom: 16 }}>
        Add credits
      </h1>

      <LowBalanceBanner onTopUp={() => setCheckoutOpen(true)} />

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
              borderRadius: "var(--r-2)",
              border:
                amountPaise === p && !customINR
                  ? "1px solid var(--accent)"
                  : "1px solid var(--border)",
              background: "transparent",
              color: "var(--fg)",
              cursor: "pointer",
            }}
          >
            ₹{p / 100}
            {bonusRate(p / 100) > 0 && (
              <span
                style={{
                  marginLeft: 4,
                  fontSize: 10,
                  fontWeight: 600,
                  color: "var(--accent)",
                }}
              >
                +5%
              </span>
            )}
          </button>
        ))}
      </div>

      <p style={{ fontSize: 12, color: "var(--fg-dim)", margin: "0 0 16px" }}>
        Get 5% bonus credits on top-ups of ₹1000 or more.
      </p>

      <input
        type="number"
        placeholder="Custom amount (INR)"
        value={customINR}
        onChange={(e) => setCustomINR(e.target.value)}
        style={{
          width: "100%",
          height: 36,
          padding: "0 12px",
          borderRadius: "var(--r-2)",
          border: "1px solid var(--border)",
          background: "var(--bg)",
          color: "var(--fg)",
          marginBottom: 16,
        }}
      />

      {lastPurchase && (
        <button
          type="button"
          onClick={() => openCheckoutFor(lastPurchase.amountINR)}
          style={{
            width: "100%",
            height: 34,
            marginBottom: 8,
            borderRadius: "var(--r-2)",
            border: "1px solid var(--accent-line)",
            background: "var(--accent-soft)",
            color: "var(--accent)",
            fontSize: 13,
            fontWeight: 500,
            cursor: "pointer",
          }}
        >
          ↻ Repeat last top-up (₹{lastPurchase.amountINR})
        </button>
      )}

      <button
        type="button"
        onClick={() => setCheckoutOpen(true)}
        disabled={!canCheckout}
        style={{
          width: "100%",
          height: 44,
          marginTop: 8,
          borderRadius: "var(--r-2)",
          border: "1px solid var(--accent-line)",
          background: "var(--accent)",
          color: "var(--accent-fg)",
          fontSize: 14,
          fontWeight: 600,
          cursor: canCheckout ? "pointer" : "default",
          opacity: canCheckout ? 1 : 0.5,
        }}
      >
        {canCheckout
          ? `Continue to checkout · ₹${checkoutAmountINR.toFixed(2)}`
          : "Enter an amount of ₹1 or more"}
      </button>

      <PurchaseHistory onBuyAgain={openCheckoutFor} />

      <AutoRechargeSettings />

      {checkoutOpen && (
        <CheckoutModal
          open
          amountINR={checkoutAmountINR}
          onClose={() => setCheckoutOpen(false)}
        />
      )}
    </div>
  );
}
