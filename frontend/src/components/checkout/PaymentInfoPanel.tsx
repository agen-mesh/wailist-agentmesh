"use client";
import type { PaymentMethod } from "./types";

const METHODS: { value: PaymentMethod; label: string }[] = [
  { value: "card", label: "Credit card" },
  { value: "paypal", label: "Paypal" },
  { value: "cod", label: "Cash on delivery" },
];

const fieldStyle = {
  width: "100%",
  height: 38,
  padding: "0 12px",
  background: "var(--bg)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  color: "var(--fg)",
  fontSize: 13,
  fontFamily: "var(--font-sans)",
  outline: "none",
} as const;

const labelStyle = {
  display: "block",
  fontSize: 12,
  fontWeight: 500,
  color: "var(--fg-muted)",
  marginBottom: 6,
} as const;

// Right-hand payment column: method selection, card fields, and the primary
// Place Order action. Card fields are shown only for the "card" method.
export function PaymentInfoPanel({
  method,
  onMethodChange,
  onPlaceOrder,
  placed,
}: {
  method: PaymentMethod;
  onMethodChange: (method: PaymentMethod) => void;
  onPlaceOrder: () => void;
  placed: boolean;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
      <div style={{ fontSize: 16, fontWeight: 600, color: "var(--fg)" }}>
        Payment Info
      </div>

      {/* Method radios */}
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {METHODS.map((m) => {
          const selected = method === m.value;
          return (
            <label
              key={m.value}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                cursor: "pointer",
                fontSize: 13,
                color: "var(--fg)",
              }}
            >
              <input
                type="radio"
                name="payment-method"
                checked={selected}
                onChange={() => onMethodChange(m.value)}
                style={{
                  position: "absolute",
                  opacity: 0,
                  width: 0,
                  height: 0,
                }}
              />
              <span
                aria-hidden
                style={{
                  width: 16,
                  height: 16,
                  borderRadius: 999,
                  border: `1px solid ${selected ? "var(--accent)" : "var(--border-strong)"}`,
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  flexShrink: 0,
                }}
              >
                {selected && (
                  <span
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: 999,
                      background: "var(--accent)",
                    }}
                  />
                )}
              </span>
              {m.label}
            </label>
          );
        })}
      </div>

      {method === "card" && (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <div>
            <label style={labelStyle} htmlFor="card-name">
              Name on Card:
            </label>
            <input
              id="card-name"
              type="text"
              placeholder="John Joe"
              style={fieldStyle}
            />
          </div>
          <div>
            <label style={labelStyle} htmlFor="card-number">
              Card Number
            </label>
            <input
              id="card-number"
              type="text"
              inputMode="numeric"
              placeholder="0000 0000 0000 1235"
              style={fieldStyle}
            />
          </div>
          <div style={{ display: "flex", gap: 12 }}>
            <div style={{ flex: 1 }}>
              <label style={labelStyle} htmlFor="card-exp">
                Expiration Date
              </label>
              <div style={{ display: "flex", gap: 8 }}>
                <input
                  id="card-exp"
                  type="text"
                  inputMode="numeric"
                  placeholder="MM"
                  style={fieldStyle}
                  aria-label="Expiration month"
                />
                <input
                  type="text"
                  inputMode="numeric"
                  placeholder="YYYY"
                  style={fieldStyle}
                  aria-label="Expiration year"
                />
              </div>
            </div>
            <div style={{ width: 96 }}>
              <label style={labelStyle} htmlFor="card-cvv">
                CVV
              </label>
              <input
                id="card-cvv"
                type="text"
                inputMode="numeric"
                placeholder="***"
                style={fieldStyle}
                aria-label="CVV"
              />
            </div>
          </div>
        </div>
      )}

      <button
        type="button"
        onClick={onPlaceOrder}
        style={{
          height: 44,
          width: "100%",
          marginTop: "auto",
          background: placed ? "var(--bg-elev-3)" : "var(--accent)",
          border: "1px solid var(--accent-line)",
          borderRadius: "var(--r-2)",
          color: placed ? "var(--accent)" : "var(--accent-fg)",
          fontSize: 14,
          fontWeight: 600,
          cursor: "pointer",
          fontFamily: "var(--font-sans)",
          transition: "background 0.18s var(--ease)",
        }}
      >
        {placed ? "✓ Order Placed" : "Place Order"}
      </button>
    </div>
  );
}
