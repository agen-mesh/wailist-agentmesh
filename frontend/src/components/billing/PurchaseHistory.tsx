"use client";
import { Card, Pill } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";
import type { PaymentMethod } from "@/components/checkout/types";

const METHOD_LABELS: Record<PaymentMethod, string> = {
  card: "Credit card",
  paypal: "Paypal",
  cod: "Cash on delivery",
};

const dateFmt = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
  timeStyle: "short",
});

// Mock billing history sourced from the local credits store. Newest first.
export function PurchaseHistory() {
  const { purchases, hydrated } = useCredits();

  // Avoid rendering store-derived rows until hydrated (SSR shows nothing).
  if (!hydrated) return null;

  return (
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
                alignItems: "center",
                justifyContent: "space-between",
                gap: 16,
                padding: "12px 16px",
                borderTop: i === 0 ? "none" : "1px solid var(--border-soft)",
              }}
            >
              <div style={{ minWidth: 0 }}>
                <div
                  style={{ fontSize: 13, color: "var(--fg)", fontWeight: 500 }}
                >
                  ₹{p.amountINR.toFixed(2)}
                  <span style={{ color: "var(--fg-dim)", fontWeight: 400 }}>
                    {" · "}
                    {METHOD_LABELS[p.method]}
                  </span>
                </div>
                <div
                  style={{
                    fontSize: 11,
                    color: "var(--fg-dim)",
                    fontFamily: "var(--font-mono)",
                    marginTop: 2,
                  }}
                >
                  {dateFmt.format(new Date(p.createdAt))}
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <span
                  style={{
                    fontSize: 13,
                    fontFamily: "var(--font-mono)",
                    color: "var(--accent)",
                    whiteSpace: "nowrap",
                  }}
                >
                  +${p.creditsUSD.toFixed(2)}
                </span>
                <Pill tone="ok">Paid</Pill>
              </div>
            </div>
          ))}
        </Card>
      )}
    </div>
  );
}
