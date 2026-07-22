"use client";
import type { OrderTotals } from "./types";

// Money summary for the credits checkout — a right-aligned totals block. Coupons
// are intentionally omitted (not offered yet) and there is no shipping, since
// credits are digital: you pay the subtotal.
export function OrderSummary({ totals }: { totals: OrderTotals }) {
  return (
    <div
      style={{
        paddingTop: 20,
        borderTop: "1px solid var(--border)",
        display: "flex",
        justifyContent: "flex-end",
      }}
    >
      <div
        style={{
          width: "100%",
          maxWidth: 280,
          display: "flex",
          flexDirection: "column",
          gap: 12,
        }}
      >
        <SummaryRow label="Subtotal" value={totals.subtotal} />
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            paddingTop: 12,
            borderTop: "1px solid var(--border)",
          }}
        >
          <span style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>
            Total Payable
          </span>
          <span
            style={{
              fontSize: 16,
              fontWeight: 700,
              color: "var(--fg)",
              fontFamily: "var(--font-mono)",
            }}
          >
            ${totals.total.toFixed(2)}
          </span>
        </div>
      </div>
    </div>
  );
}

function SummaryRow({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ display: "flex", justifyContent: "space-between" }}>
      <span style={{ fontSize: 13, color: "var(--fg-muted)" }}>{label}</span>
      <span
        style={{
          fontSize: 13,
          color: "var(--fg)",
          fontFamily: "var(--font-mono)",
        }}
      >
        ${value.toFixed(2)}
      </span>
    </div>
  );
}
