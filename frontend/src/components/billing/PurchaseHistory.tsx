"use client";
import { useEffect, useState } from "react";
import { Card, Pill } from "@/components/ui";
import { rowBtn } from "@/components/ui/buttons";
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

const dateFmt = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
  timeStyle: "short",
});

// STATUS_PILLS maps credit_ledger.status to what the row shows. Only
// 'completed' actually granted credits; the others are surfaced rather than
// filtered out so a user whose payment did not land can see which stage it
// stopped at instead of an empty page.
const STATUS_PILLS: Record<
  PurchaseStatus,
  { label: string; tone: "ok" | "warm" | "danger" }
> = {
  completed: { label: "Paid", tone: "ok" },
  pending: { label: "Pending", tone: "warm" },
  partial: { label: "Partial", tone: "warm" },
  refunded: { label: "Refunded", tone: "warm" },
  failed: { label: "Failed", tone: "danger" },
  expired: { label: "Expired", tone: "danger" },
};

// Billing history from credit_ledger via GET /credits/purchases. Newest first.
export function PurchaseHistory({
  onBuyAgain,
}: {
  onBuyAgain: (amountINR: number) => void;
}) {
  const { purchases, purchasesKnown, refreshPurchases } = useCredits();
  const [receipt, setReceipt] = useState<Purchase | null>(null);

  useEffect(() => {
    void refreshPurchases();
  }, [refreshPurchases]);

  // Nothing until the server has answered: an empty list before that is "not
  // asked yet", and rendering "No purchases yet" for it tells a paying user
  // their receipts are gone.
  if (!purchasesKnown) return null;

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
                    {p.amountINR !== undefined
                      ? `\u20B9${p.amountINR.toFixed(2)}`
                      : `$${(p.amountUSD ?? 0).toFixed(2)}`}
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
                    <Pill tone={STATUS_PILLS[p.status].tone}>
                      {STATUS_PILLS[p.status].label}
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
                    style={rowBtn}
                  >
                    Receipt
                  </button>
                  {/* Only an INR row can be repeated -- the checkout is
                      driven by an INR amount, which a crypto top-up has
                      none of. */}
                  {p.amountINR !== undefined && (
                    <button
                      type="button"
                      onClick={() => onBuyAgain(p.amountINR!)}
                      style={rowBtn}
                    >
                      Buy again
                    </button>
                  )}
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
