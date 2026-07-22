"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { IconClose } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";
import type { CartItem, PaymentMethod } from "./types";
import { buildCreditCart, computeTotals } from "./mockData";
import { CartItemRow } from "./CartItemRow";
import { OrderSummary } from "./OrderSummary";
import { PaymentInfoPanel } from "./PaymentInfoPanel";

// Scoped rules that inline styles can't express: the native <dialog> backdrop,
// the dialog as a flex column so its body scrolls, and the responsive collapse
// of the two-column layout on narrow viewports.
const DIALOG_CSS = `
.checkout-dialog { display: flex; flex-direction: column; max-height: 90vh; }
.checkout-dialog::backdrop { background: rgba(8,7,12,0.7); backdrop-filter: blur(4px); }
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
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [items, setItems] = useState<CartItem[]>(() =>
    buildCreditCart(amountINR),
  );
  const [method, setMethod] = useState<PaymentMethod>("card");
  const { addPurchase } = useCredits();

  const totals = useMemo(() => computeTotals(items), [items]);

  // Drive the native dialog. showModal() gives focus trapping and the backdrop
  // for free. Escape fires the dialog's "cancel" event — sync parent state from
  // it (the ✕ and backdrop call onClose directly). On unmount, close the native
  // dialog so the top layer is released cleanly.
  useEffect(() => {
    const dlg = dialogRef.current;
    if (!dlg) return;
    if (open && !dlg.open) dlg.showModal();
    const onCancel = () => onClose();
    dlg.addEventListener("cancel", onCancel);
    return () => {
      dlg.removeEventListener("cancel", onCancel);
      if (dlg.open) dlg.close();
    };
  }, [open, onClose]);

  const handleQuantityChange = (id: string, quantity: number) => {
    setItems((prev) =>
      prev.map((item) => (item.id === id ? { ...item, quantity } : item)),
    );
  };

  const handleRemove = (id: string) => {
    setItems((prev) => prev.filter((item) => item.id !== id));
  };

  return (
    <dialog
      ref={dialogRef}
      className="checkout-dialog"
      aria-label="Checkout"
      onClick={(e) => {
        // Clicks on the backdrop land on the <dialog> element itself.
        if (e.target === dialogRef.current) onClose();
      }}
      style={{
        padding: 0,
        border: "1px solid var(--border-strong)",
        borderRadius: "var(--r-4)",
        background: "var(--bg-elev-1)",
        color: "var(--fg)",
        width: "min(980px, calc(100vw - 48px))",
        maxWidth: "min(980px, calc(100vw - 48px))",
        boxShadow: "0 24px 64px rgba(0,0,0,0.5)",
      }}
    >
      <style>{DIALOG_CSS}</style>
      <div style={{ flex: 1, minHeight: 0, overflowY: "auto", padding: 24 }}>
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
              <span>Item</span>
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
              <PaymentInfoPanel
                method={method}
                onMethodChange={setMethod}
                payable={totals.total > 0}
                onPaid={() =>
                  addPurchase({ amountINR: totals.total, method })
                }
              />
            </div>
          </div>
        </div>
      </div>
    </dialog>
  );
}
