"use client";
import { useEffect, useRef } from "react";
import type { Purchase } from "@/lib/credits/types";
import type { PaymentMethod } from "@/components/checkout/types";

const METHOD_LABELS: Record<PaymentMethod, string> = {
  card: "Credit card",
  paypal: "Paypal",
  cod: "Cash on delivery",
};

const dateFmt = new Intl.DateTimeFormat("en", {
  dateStyle: "long",
  timeStyle: "short",
});

// The modal is dark on-screen (design system), but a printed receipt must be
// legible on paper, so print inverts the receipt to black-on-white and hides
// everything else on the page.
const RECEIPT_CSS = `
.receipt-dialog::backdrop { background: rgba(8,7,12,0.7); backdrop-filter: blur(4px); }
@media print {
  body > *:not(.receipt-dialog) { display: none !important; }
  .receipt-dialog { position: static !important; box-shadow: none !important; border: none !important; }
  .receipt-dialog, .receipt-dialog * { background: #fff !important; color: #000 !important; border-color: #ddd !important; }
}
`;

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        gap: 16,
        padding: "8px 0",
        borderBottom: "1px solid var(--border-soft)",
        fontSize: 13,
      }}
    >
      <span style={{ color: "var(--fg-muted)" }}>{label}</span>
      <span style={{ color: "var(--fg)", fontFamily: "var(--font-mono)" }}>
        {value}
      </span>
    </div>
  );
}

export function Receipt({
  purchase,
  onClose,
}: {
  purchase: Purchase;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dlg = dialogRef.current;
    if (!dlg) return;
    if (!dlg.open) dlg.showModal();
    const onCancel = () => onClose();
    dlg.addEventListener("cancel", onCancel);
    return () => {
      dlg.removeEventListener("cancel", onCancel);
      if (dlg.open) dlg.close();
    };
  }, [onClose]);

  return (
    <dialog
      ref={dialogRef}
      className="receipt-dialog"
      aria-label="Receipt"
      onClick={(e) => {
        if (e.target === dialogRef.current) onClose();
      }}
      style={{
        padding: 0,
        border: "1px solid var(--border-strong)",
        borderRadius: "var(--r-4)",
        background: "var(--bg-elev-1)",
        color: "var(--fg)",
        width: "min(420px, calc(100vw - 48px))",
        maxWidth: "min(420px, calc(100vw - 48px))",
        boxShadow: "0 24px 64px rgba(0,0,0,0.5)",
      }}
    >
      <style>{RECEIPT_CSS}</style>
      <div style={{ padding: 24 }}>
        <div
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "space-between",
            marginBottom: 16,
          }}
        >
          <span style={{ fontSize: 16, fontWeight: 700, color: "var(--fg)" }}>
            AgentMesh
          </span>
          <span
            style={{
              fontSize: 12,
              textTransform: "uppercase",
              letterSpacing: "0.08em",
              color: "var(--fg-dim)",
            }}
          >
            Receipt
          </span>
        </div>

        <div>
          <Row label="Receipt ID" value={purchase.id} />
          <Row
            label="Date"
            value={dateFmt.format(new Date(purchase.createdAt))}
          />
          <Row
            label="Amount paid"
            value={`₹${purchase.amountINR.toFixed(2)}`}
          />
          <Row
            label="Credits added"
            value={`$${purchase.creditsUSD.toFixed(2)}`}
          />
          <Row label="Method" value={METHOD_LABELS[purchase.method]} />
          <Row label="Status" value="Paid" />
        </div>

        <p style={{ fontSize: 11, color: "var(--fg-dim)", margin: "14px 0 0" }}>
          Mock receipt — not a valid tax invoice.
        </p>

        <div
          style={{
            display: "flex",
            gap: 10,
            marginTop: 20,
            justifyContent: "flex-end",
          }}
        >
          <button
            type="button"
            onClick={() => window.print()}
            style={{
              height: 36,
              padding: "0 16px",
              borderRadius: "var(--r-2)",
              border: "1px solid var(--accent-line)",
              background: "var(--accent)",
              color: "var(--accent-fg)",
              fontSize: 13,
              fontWeight: 600,
              cursor: "pointer",
            }}
          >
            Print
          </button>
          <button
            type="button"
            onClick={onClose}
            style={{
              height: 36,
              padding: "0 16px",
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
    </dialog>
  );
}
