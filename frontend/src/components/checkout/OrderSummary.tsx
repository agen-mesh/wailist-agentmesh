"use client";
import type { OrderTotals } from "./types";

// Bottom band of the cart card: coupon entry on the left, money summary on the
// right — matching the reference arrangement, dark-themed.
export function OrderSummary({
  totals,
  coupon,
  onCouponChange,
  onApplyCoupon,
}: {
  totals: OrderTotals;
  coupon: string;
  onCouponChange: (value: string) => void;
  onApplyCoupon: () => void;
}) {
  return (
    <div
      className="checkout-summary"
      style={{
        paddingTop: 20,
        borderTop: "1px solid var(--border)",
      }}
    >
      {/* Coupon */}
      <div>
        <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>
          Coupon Code
        </div>
        <div
          style={{ fontSize: 12, color: "var(--fg-dim)", margin: "4px 0 12px" }}
        >
          Enter code to get discount instantly
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <input
            type="text"
            value={coupon}
            onChange={(e) => onCouponChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") onApplyCoupon();
            }}
            placeholder="Add discount code"
            aria-label="Discount code"
            style={{
              flex: 1,
              minWidth: 0,
              height: 36,
              padding: "0 12px",
              background: "var(--bg)",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-2)",
              color: "var(--fg)",
              fontSize: 13,
              fontFamily: "var(--font-sans)",
              outline: "none",
            }}
          />
          <button
            type="button"
            onClick={onApplyCoupon}
            style={{
              height: 36,
              padding: "0 18px",
              background: "var(--accent)",
              border: "1px solid var(--accent-line)",
              borderRadius: "var(--r-2)",
              color: "var(--accent-fg)",
              fontSize: 13,
              fontWeight: 600,
              cursor: "pointer",
              fontFamily: "var(--font-sans)",
            }}
          >
            Apply
          </button>
        </div>
      </div>

      {/* Totals */}
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <SummaryRow label="Subtotal" value={totals.subtotal} />
        <SummaryRow label="Shipping Cost" value={totals.shipping} />
        <SummaryRow label="Discount" value={-totals.discount} />
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
  const sign = value < 0 ? "-$" : "$";
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
        {sign}
        {Math.abs(value).toFixed(2)}
      </span>
    </div>
  );
}
