"use client";
import { useState } from "react";
import { Card, Pill } from "@/components/ui";
import { rowBtn } from "@/components/ui/buttons";
import { useCredits } from "@/lib/credits/store";
import { Receipt } from "./Receipt";
import type { Purchase } from "@/lib/credits/types";
import type { PaymentMethod } from "@/components/checkout/types";

const METHOD_LABELS: Record<PaymentMethod, string> = {
  cashfree: "Cashfree",
  nowpayments: "NOWPayments",
  paypal: "PayPal",
  stripe: "Stripe",
};

const dateFmt = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
  timeStyle: "short",
});

// Mock billing history sourced from the local credits store. Newest first.
export function PurchaseHistory({
  onBuyAgain,
}: {
  onBuyAgain: (amountINR: number) => void;
}) {
  const { purchases, hydrated } = useCredits();
  const [receipt, setReceipt] = useState<Purchase | null>(null);

  // Avoid rendering store-derived rows until hydrated (SSR shows nothing).
  if (!hydrated) return null;

  return (
    <>
      <div style={{ marginTop: 32 }}>
        <h2
          style={{
            fontSize: 15,
            fontWeight: 600,
            color: "var(--fg)",
            marginBottom: 12,
          }}
        >
          Billing history
        </h2>

        {purchases.length === 0 ? (
          <p style={{ fontSize: 13, color: "var(--fg-dim)", margin: 0 }}>
            No purchases yet.
          </p>
        ) : (
          <Card style={{ padding: 0, overflow: "hidden" }}>
            {purchases.map((p, i) => (
              <div
                key={p.id}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: 8,
                  padding: "12px 16px",
                  borderTop: i === 0 ? "none" : "1px solid var(--border-soft)",
                }}
              >
                {/* Amount + credits granted + status */}
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 8,
                  }}
                >
                  <span
                    style={{
                      fontSize: 13,
                      fontWeight: 600,
                      color: "var(--fg)",
                      fontFamily: "var(--font-mono)",
                      fontVariantNumeric: "tabular-nums",
                    }}
                  >
                    ₹{p.amountINR.toFixed(2)}
                  </span>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      flexShrink: 0,
                    }}
                  >
                    <span
                      style={{
                        fontSize: 13,
                        fontFamily: "var(--font-mono)",
                        color: "var(--accent)",
                        fontVariantNumeric: "tabular-nums",
                      }}
                    >
                      +${p.creditsUSD.toFixed(2)}
                    </span>
                    <Pill tone="ok">Paid</Pill>
                  </div>
                </div>

                {/* Method · date */}
                <div
                  style={{
                    fontSize: 11,
                    color: "var(--fg-dim)",
                    fontFamily: "var(--font-mono)",
                  }}
                >
                  {METHOD_LABELS[p.method]} ·{" "}
                  {dateFmt.format(new Date(p.createdAt))}
                </div>

                {/* Actions */}
                <div style={{ display: "flex", gap: 8 }}>
                  <button
                    type="button"
                    onClick={() => setReceipt(p)}
                    style={rowBtn}
                  >
                    Receipt
                  </button>
                  <button
                    type="button"
                    onClick={() => onBuyAgain(p.amountINR)}
                    style={rowBtn}
                  >
                    Buy again
                  </button>
                </div>
              </div>
            ))}
          </Card>
        )}
      </div>
      {receipt && (
        <Receipt purchase={receipt} onClose={() => setReceipt(null)} />
      )}
    </>
  );
}
