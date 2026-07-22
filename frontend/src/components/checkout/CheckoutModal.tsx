"use client";
import { useEffect, useMemo, useState } from "react";
import { IconClose } from "@/components/ui";
import type { CartItem, PaymentMethod } from "./types";
import { buildCreditCart, computeTotals } from "./mockData";
import { CartItemRow } from "./CartItemRow";
import { OrderSummary } from "./OrderSummary";
import { PaymentInfoPanel } from "./PaymentInfoPanel";

// Scoped responsive rules — inline styles can't express media queries, so the
// two side-by-side grids collapse to a single column on narrow viewports here.
const RESPONSIVE_CSS = `
.checkout-split { display: grid; grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr); gap: 20px; }
.checkout-cart-head { display: grid; grid-template-columns: 24px 1fr auto auto; gap: 16px; }
@media (max-width: 860px) {
  .checkout-split { grid-template-columns: minmax(0, 1fr); }
}
`;

export function CheckoutModal({
  open,
  amountINR,
  onClose,
}: {
  open: boolean;
  amountINR: number;
  onClose: () => void;
}) {
  const [items, setItems] = useState<CartItem[]>(() =>
    buildCreditCart(amountINR),
  );
  const [method, setMethod] = useState<PaymentMethod>("card");

  const totals = useMemo(() => computeTotals(items), [items]);

  // Close on Escape while open.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const handleQuantityChange = (id: string, quantity: number) => {
    setItems((prev) =>
      prev.map((item) => (item.id === id ? { ...item, quantity } : item)),
    );
  };

  const handleRemove = (id: string) => {
    setItems((prev) => prev.filter((item) => item.id !== id));
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Checkout"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(8,7,12,0.7)",
        backdropFilter: "blur(4px)",
        zIndex: 100,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 24,
      }}
    >
      <style>{RESPONSIVE_CSS}</style>
      <div
        style={{
          width: "100%",
          maxWidth: 980,
          maxHeight: "90vh",
          overflowY: "auto",
          background: "var(--bg-elev-1)",
          border: "1px solid var(--border-strong)",
          borderRadius: "var(--r-4)",
          padding: 24,
          boxShadow: "0 24px 64px rgba(0,0,0,0.5)",
        }}
      >
        {/* Header */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            marginBottom: 20,
          }}
        >
          <h2
            style={{
              fontSize: 18,
              fontWeight: 700,
              color: "var(--fg)",
              margin: 0,
            }}
          >
            Checkout
          </h2>
          <button
            type="button"
            aria-label="Close checkout"
            onClick={onClose}
            style={{
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              width: 32,
              height: 32,
              background: "transparent",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-2)",
              color: "var(--fg-muted)",
              cursor: "pointer",
            }}
          >
            <IconClose size={14} />
          </button>
        </div>

        <div className="checkout-split">
          {/* Cart card */}
          <div
            style={{
              background: "var(--bg-elev-2)",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-3)",
              padding: 20,
            }}
          >
            <div
              className="checkout-cart-head"
              style={{
                paddingBottom: 12,
                fontSize: 12,
                fontWeight: 500,
                color: "var(--fg-muted)",
              }}
            >
              <span />
              <span>Product</span>
              <span style={{ textAlign: "center" }}>Quantity</span>
              <span style={{ textAlign: "right", minWidth: 72 }}>Price</span>
            </div>

            {items.length === 0 ? (
              <div
                style={{
                  padding: "32px 0",
                  textAlign: "center",
                  color: "var(--fg-dim)",
                  fontSize: 13,
                  borderTop: "1px solid var(--border-soft)",
                }}
              >
                Your cart is empty.
              </div>
            ) : (
              items.map((item) => (
                <CartItemRow
                  key={item.id}
                  item={item}
                  onQuantityChange={handleQuantityChange}
                  onRemove={handleRemove}
                />
              ))
            )}

            <div style={{ marginTop: 8 }}>
              <OrderSummary totals={totals} />
            </div>
          </div>

          {/* Payment card */}
          <div
            style={{
              background: "var(--bg-elev-2)",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-3)",
              padding: 20,
              display: "flex",
            }}
          >
            <div
              style={{
                width: "100%",
                display: "flex",
                flexDirection: "column",
              }}
            >
              <PaymentInfoPanel method={method} onMethodChange={setMethod} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
