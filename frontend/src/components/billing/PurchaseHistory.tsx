"use client";
import { useState } from "react";
import { Card, Pill, ghostBtnSm } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";
import { Receipt } from "./Receipt";
import type { Purchase, PurchaseStatus } from "@/lib/credits/types";
import type { PaymentMethod } from "@/components/checkout/types";

const METHOD_LABELS: Record<PaymentMethod, string> = {
  cashfree: "Cashfree",
  nowpayments: "NOWPayments",
  paypal: "PayPal",
  stripe: "Stripe",
};

// Real DB-backed history can be more than just "paid", so the pill reflects the
// actual ledger status instead of always claiming success.
const STATUS_PILL: Record<
  PurchaseStatus,
  { label: string; tone: "ok" | "default" }
> = {
  paid: { label: "Paid", tone: "ok" },
  pending: { label: "Pending", tone: "default" },
  failed: { label: "Failed", tone: "default" },
  refunded: { label: "Refunded", tone: "default" },
};

const dateFmt = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
  timeStyle: "short",
});

const rowBtnStyle: React.CSSProperties = {
  height: 28,
  padding: "0 12px",
  borderRadius: "var(--r-2)",
  border: "1px solid var(--border-strong)",
  background: "transparent",
  color: "var(--fg-muted)",
  fontSize: 12,
  fontWeight: 500,
  cursor: "pointer",
  whiteSpace: "nowrap",
};

// Mock billing history sourced from the local credits store. Newest first.
export function PurchaseHistory({
  onBuyAgain,
}: {
  onBuyAgain: (amountINR: number) => void;
}) {
  const { purchases, hydrated, loadError, refresh } = useCredits();
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

        {loadError ? (
          // "No purchases yet" would be a lie here — the history never loaded.
          <div
            role="alert"
            style={{
              padding: 24,
              textAlign: "center",
              border: "1px dashed var(--danger-line)",
              borderRadius: "var(--r-3)",
              background: "var(--danger-soft)",
            }}
          >
            <div
              style={{
                color: "var(--danger)",
                fontFamily: "var(--font-mono)",
                fontSize: 12,
                marginBottom: 6,
              }}
            >
              couldn&apos;t load billing history
            </div>
            <div
              style={{
                color: "var(--fg-muted)",
                fontSize: 13,
                marginBottom: 14,
              }}
            >
              This is different from having no purchases yet.
            </div>
            <button onClick={refresh} style={ghostBtnSm}>
              Retry
            </button>
          </div>
        ) : purchases.length === 0 ? (
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
                    <Pill tone={STATUS_PILL[p.status].tone}>
                      {STATUS_PILL[p.status].label}
                    </Pill>
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
                    style={rowBtnStyle}
                  >
                    Receipt
                  </button>
                  <button
                    type="button"
                    onClick={() => onBuyAgain(p.amountINR)}
                    style={rowBtnStyle}
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
