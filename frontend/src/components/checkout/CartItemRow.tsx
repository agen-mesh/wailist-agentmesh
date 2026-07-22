"use client";
import type { CartItem } from "./types";
import { IconClose, IconWallet } from "@/components/ui";

// A single cart line. Layout mirrors the reference (remove · thumb · title /
// variant · quantity stepper · price) but styling is pulled entirely from the
// app's dark design tokens.
export function CartItemRow({
  item,
  onQuantityChange,
  onRemove,
}: {
  item: CartItem;
  onQuantityChange: (id: string, quantity: number) => void;
  onRemove: (id: string) => void;
}) {
  const lineTotal = item.unitPrice * item.quantity;

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "24px 1fr auto auto",
        alignItems: "center",
        gap: 16,
        padding: "16px 0",
        borderTop: "1px solid var(--border-soft)",
      }}
    >
      <button
        type="button"
        aria-label={`Remove ${item.title}`}
        onClick={() => onRemove(item.id)}
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 24,
          height: 24,
          background: "transparent",
          border: "none",
          color: "var(--fg-dim)",
          cursor: "pointer",
          borderRadius: "var(--r-1)",
        }}
      >
        <IconClose size={12} />
      </button>

      <div
        style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0 }}
      >
        <div
          aria-hidden
          style={{
            flexShrink: 0,
            width: 44,
            height: 44,
            borderRadius: "var(--r-2)",
            background:
              "linear-gradient(135deg, var(--bg-elev-3), var(--bg-elev-2))",
            border: "1px solid var(--border)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--accent)",
          }}
        >
          <IconWallet size={20} />
        </div>
        <div style={{ minWidth: 0 }}>
          <div
            style={{
              fontSize: 13,
              fontWeight: 500,
              color: "var(--fg)",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {item.title}
          </div>
          <div style={{ fontSize: 12, color: "var(--fg-dim)", marginTop: 2 }}>
            {item.detail}
          </div>
        </div>
      </div>

      <QuantityStepper
        quantity={item.quantity}
        onChange={(q) => onQuantityChange(item.id, q)}
        label={item.title}
      />

      <div
        style={{
          fontSize: 14,
          fontWeight: 600,
          color: "var(--fg)",
          fontFamily: "var(--font-mono)",
          minWidth: 72,
          textAlign: "right",
        }}
      >
        ${lineTotal.toFixed(2)}
      </div>
    </div>
  );
}

function QuantityStepper({
  quantity,
  onChange,
  label,
}: {
  quantity: number;
  onChange: (quantity: number) => void;
  label: string;
}) {
  const btn = {
    width: 26,
    height: 26,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    background: "transparent",
    border: "none",
    color: "var(--fg-muted)",
    cursor: "pointer",
    fontSize: 15,
    lineHeight: 1,
  } as const;

  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        border: "1px solid var(--border)",
        borderRadius: "var(--r-2)",
        background: "var(--bg-elev-2)",
        overflow: "hidden",
      }}
    >
      <button
        type="button"
        aria-label={`Decrease quantity of ${label}`}
        onClick={() => onChange(Math.max(1, quantity - 1))}
        disabled={quantity <= 1}
        style={{ ...btn, opacity: quantity <= 1 ? 0.4 : 1 }}
      >
        −
      </button>
      <span
        aria-live="polite"
        style={{
          minWidth: 32,
          textAlign: "center",
          fontSize: 13,
          fontFamily: "var(--font-mono)",
          color: "var(--fg)",
        }}
      >
        {quantity}
      </span>
      <button
        type="button"
        aria-label={`Increase quantity of ${label}`}
        onClick={() => onChange(quantity + 1)}
        style={btn}
      >
        +
      </button>
    </div>
  );
}
