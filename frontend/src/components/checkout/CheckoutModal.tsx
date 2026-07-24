"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { IconClose } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";
import type { Purchase } from "@/lib/credits/types";
import type { PaymentMethod } from "./types";
import { DEFAULT_PROVIDER } from "./paymentProviders";
import { buildCreditCart, computeTotals } from "./mockData";
import { CartItemRow } from "./CartItemRow";
import { OrderSummary } from "./OrderSummary";
import { PaymentInfoPanel } from "./PaymentInfoPanel";

// Scoped rules that inline styles can't express: the native <dialog> backdrop,
// the dialog as a flex column so its body scrolls, and the responsive collapse
// of the two-column layout on narrow viewports.
const DIALOG_CSS = `
/* margin:auto re-centres the modal — Tailwind's preflight resets the native
   dialog's default centring margin, which otherwise pins it to the top-left. */
.checkout-dialog { display: flex; flex-direction: column; max-height: 90vh; margin: auto; }
.checkout-dialog[open] { animation: checkout-in 0.24s var(--ease); }
.checkout-dialog::backdrop { background: rgba(8,7,12,0.7); backdrop-filter: blur(4px); }
.checkout-split { display: grid; grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr); gap: 20px; }
.checkout-check { animation: checkout-pulse 2.4s var(--ease) infinite; }
@keyframes checkout-in {
  from { opacity: 0; transform: translateY(8px) scale(0.985); }
  to { opacity: 1; transform: none; }
}
@keyframes checkout-pulse {
  0%, 100% { box-shadow: 0 0 26px 2px var(--accent-glow); }
  50% { box-shadow: 0 0 10px 0 var(--accent-glow); }
}
@media (max-width: 860px) {
  .checkout-split { grid-template-columns: minmax(0, 1fr); }
}
@media (prefers-reduced-motion: reduce) {
  .checkout-dialog[open], .checkout-check { animation: none; }
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
  const items = useMemo(() => buildCreditCart(amountINR), [amountINR]);
  const [method, setMethod] = useState<PaymentMethod>(DEFAULT_PROVIDER);
  const { addPurchase, balanceUSD } = useCredits();
  const router = useRouter();
  const [confirmation, setConfirmation] = useState<Purchase | null>(null);

  const totals = useMemo(() => computeTotals(items), [items]);

  const handlePaid = () => {
    setConfirmation(addPurchase({ amountINR: totals.total, method }));
  };

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
        {confirmation ? (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              textAlign: "center",
              gap: 12,
              padding: "40px 16px",
            }}
          >
            <div
              className="checkout-check"
              style={{
                width: 52,
                height: 52,
                borderRadius: 999,
                background: "var(--accent-soft)",
                border: "1px solid var(--accent-line)",
                color: "var(--accent)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: 24,
              }}
            >
              ✓
            </div>
            <h2
              style={{
                fontSize: 18,
                fontWeight: 700,
                color: "var(--fg)",
                margin: 0,
              }}
            >
              Payment successful
            </h2>
            <p style={{ fontSize: 13, color: "var(--fg-muted)", margin: 0 }}>
              ${confirmation.creditsUSD.toFixed(2)} credits added to your
              wallet.
            </p>
            <div
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 13,
                color: "var(--fg)",
                background: "var(--bg-elev-2)",
                border: "1px solid var(--border)",
                borderRadius: "var(--r-2)",
                padding: "8px 14px",
              }}
            >
              New balance: ${balanceUSD.toFixed(2)}
            </div>
            <div style={{ display: "flex", gap: 10, marginTop: 8 }}>
              <button
                type="button"
                onClick={() => router.push("/usage")}
                style={{
                  height: 38,
                  padding: "0 18px",
                  borderRadius: "var(--r-2)",
                  border: "1px solid var(--accent-line)",
                  background: "var(--accent)",
                  color: "var(--accent-fg)",
                  fontSize: 13,
                  fontWeight: 600,
                  cursor: "pointer",
                }}
              >
                Go to Usage
              </button>
              <button
                type="button"
                onClick={onClose}
                style={{
                  height: 38,
                  padding: "0 18px",
                  borderRadius: "var(--r-2)",
                  border: "1px solid var(--border-strong)",
                  background: "transparent",
                  color: "var(--fg-muted)",
                  fontSize: 13,
                  fontWeight: 500,
                  cursor: "pointer",
                }}
              >
                Close
              </button>
            </div>
          </div>
        ) : (
          <>
            {/* Header */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                marginBottom: 20,
              }}
            >
              <div>
                <h2
                  style={{
                    fontSize: 19,
                    fontWeight: 700,
                    color: "var(--fg)",
                    margin: 0,
                    letterSpacing: "-0.01em",
                  }}
                >
                  Checkout
                </h2>
                <p
                  style={{
                    margin: "3px 0 0",
                    fontSize: 12.5,
                    color: "var(--fg-muted)",
                  }}
                >
                  Complete your credit top-up
                </p>
              </div>
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
                  style={{
                    paddingBottom: 4,
                    fontSize: 12,
                    fontWeight: 600,
                    color: "var(--fg-muted)",
                  }}
                >
                  Order summary
                </div>

                {items.map((item) => (
                  <CartItemRow key={item.id} item={item} />
                ))}

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
                    amountINR={totals.total}
                    payable={totals.total > 0}
                    onPaid={handlePaid}
                  />
                </div>
              </div>
            </div>
          </>
        )}
      </div>
    </dialog>
  );
}
