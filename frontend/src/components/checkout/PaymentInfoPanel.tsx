"use client";
import { useState, type CSSProperties } from "react";
import type { PaymentMethod } from "./types";

const METHODS: { value: PaymentMethod; label: string }[] = [
  { value: "card", label: "Credit card" },
  { value: "paypal", label: "Paypal" },
  { value: "cod", label: "Cash on delivery" },
];

const labelStyle: CSSProperties = {
  display: "block",
  fontSize: 12,
  fontWeight: 500,
  color: "var(--fg-muted)",
  marginBottom: 6,
};

// Field style; borders turn red once a field has an invalid value.
function fieldStyle(invalid: boolean): CSSProperties {
  return {
    width: "100%",
    height: 38,
    padding: "0 12px",
    background: "var(--bg)",
    border: `1px solid ${invalid ? "var(--danger)" : "var(--border)"}`,
    borderRadius: "var(--r-2)",
    color: "var(--fg)",
    fontSize: 13,
    fontFamily: "var(--font-sans)",
    outline: "none",
  };
}

// Luhn checksum — catches transposed/mistyped card numbers immediately.
function luhn(digits: string): boolean {
  let sum = 0;
  let alt = false;
  for (let i = digits.length - 1; i >= 0; i--) {
    let d = digits.charCodeAt(i) - 48;
    if (d < 0 || d > 9) return false;
    if (alt) {
      d *= 2;
      if (d > 9) d -= 9;
    }
    sum += d;
    alt = !alt;
  }
  return sum % 10 === 0;
}

// Group digits in fours for readability, e.g. "4242 4242 4242 4242".
function formatCardNumber(raw: string): string {
  const digits = raw.replace(/\D/g, "").slice(0, 19);
  return digits.replace(/(\d{4})(?=\d)/g, "$1 ");
}

// Best-effort network detection from the leading digits (India-relevant set).
function detectNetwork(digits: string): string | null {
  if (/^4/.test(digits)) return "Visa";
  if (/^3[47]/.test(digits)) return "Amex";
  if (/^(5[1-5]|2[2-7])/.test(digits)) return "Mastercard";
  if (/^(6|81|82|508)/.test(digits)) return "RuPay";
  return null;
}

type PayStatus = "idle" | "processing" | "success";

// Right-hand payment column: method selection, card fields, and the pay action.
// Payment can only complete via a valid credit card with something to pay for;
// on success the button reads "Payment Successful". Card fields render only for
// the "card" method.
export function PaymentInfoPanel({
  method,
  onMethodChange,
  payable,
}: {
  method: PaymentMethod;
  onMethodChange: (method: PaymentMethod) => void;
  payable: boolean;
}) {
  const [name, setName] = useState("");
  const [number, setNumber] = useState("");
  const [numberTouched, setNumberTouched] = useState(false);
  const [expMonth, setExpMonth] = useState("");
  const [expYear, setExpYear] = useState("");
  const [cvv, setCvv] = useState("");
  const [status, setStatus] = useState<PayStatus>("idle");

  const digits = number.replace(/\D/g, "");
  const network = detectNetwork(digits);
  const now = new Date();
  const monthNum = Number(expMonth);
  const yearNum = Number(expYear);

  const expectedLen = network === "Amex" ? 15 : 16;
  const nameOk = name.trim().length > 1;
  const numberOk = digits.length === expectedLen && luhn(digits);
  const monthOk = monthNum >= 1 && monthNum <= 12;
  const yearOk = expYear.length === 4 && yearNum >= now.getFullYear();
  const notExpired =
    monthOk &&
    yearOk &&
    (yearNum > now.getFullYear() || monthNum >= now.getMonth() + 1);
  const cvvOk = /^\d{3,4}$/.test(cvv);
  const cardValid =
    nameOk && numberOk && monthOk && yearOk && notExpired && cvvOk;

  // Per-field error hints. Card number waits for blur so it doesn't flash red
  // mid-typing; short fields flag as soon as their value can't be valid.
  const numberErr = numberTouched && digits.length > 0 && !numberOk;
  const expErr =
    (expMonth.length > 0 && !monthOk) ||
    (expYear.length > 0 && (!yearOk || (monthOk && yearOk && !notExpired)));
  const cvvErr = cvv.length > 0 && !cvvOk;

  const canPay = method === "card" && cardValid && payable && status === "idle";

  const handlePay = () => {
    if (!canPay) return;
    setStatus("processing");
    // Mock gateway round-trip; a real integration would await verification.
    window.setTimeout(() => setStatus("success"), 900);
  };

  const isSuccess = status === "success";
  const disabled =
    status !== "idle" || method !== "card" || !cardValid || !payable;

  const buttonLabel = isSuccess
    ? "✓ Payment Successful"
    : status === "processing"
      ? "Processing…"
      : "Place Order";

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
              Name on Card
            </label>
            <input
              id="card-name"
              type="text"
              placeholder="John Joe"
              value={name}
              onChange={(e) => setName(e.target.value)}
              style={fieldStyle(false)}
            />
          </div>
          <div>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                marginBottom: 6,
              }}
            >
              <label
                style={{ ...labelStyle, marginBottom: 0 }}
                htmlFor="card-number"
              >
                Card Number
              </label>
              {network && (
                <span
                  style={{
                    fontSize: 11,
                    fontWeight: 600,
                    color: "var(--accent)",
                  }}
                >
                  {network}
                </span>
              )}
            </div>
            <input
              id="card-number"
              type="text"
              inputMode="numeric"
              autoComplete="cc-number"
              maxLength={23}
              placeholder="0000 0000 0000 0000"
              value={number}
              onChange={(e) => setNumber(formatCardNumber(e.target.value))}
              onBlur={() => setNumberTouched(true)}
              aria-invalid={numberErr}
              style={fieldStyle(numberErr)}
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
                  maxLength={2}
                  placeholder="MM"
                  value={expMonth}
                  onChange={(e) =>
                    setExpMonth(e.target.value.replace(/\D/g, ""))
                  }
                  aria-invalid={expErr}
                  aria-label="Expiration month"
                  style={fieldStyle(expErr)}
                />
                <input
                  type="text"
                  inputMode="numeric"
                  maxLength={4}
                  placeholder="YYYY"
                  value={expYear}
                  onChange={(e) =>
                    setExpYear(e.target.value.replace(/\D/g, ""))
                  }
                  aria-invalid={expErr}
                  aria-label="Expiration year"
                  style={fieldStyle(expErr)}
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
                maxLength={4}
                placeholder="123"
                value={cvv}
                onChange={(e) => setCvv(e.target.value.replace(/\D/g, ""))}
                aria-invalid={cvvErr}
                aria-label="CVV"
                style={fieldStyle(cvvErr)}
              />
            </div>
          </div>
        </div>
      )}

      <div style={{ marginTop: "auto" }}>
        {/* Trust signal — reassurance right at the point of payment. */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: 6,
            marginBottom: 12,
            color: "var(--fg-dim)",
            fontSize: 12,
          }}
        >
          <svg
            width="12"
            height="12"
            viewBox="0 0 16 16"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="3"
              y="7"
              width="10"
              height="7"
              rx="1.5"
              stroke="currentColor"
              strokeWidth="1.3"
            />
            <path
              d="M5 7V5a3 3 0 0 1 6 0v2"
              stroke="currentColor"
              strokeWidth="1.3"
            />
          </svg>
          Secured by Razorpay · details are encrypted
        </div>

        <button
          type="button"
          onClick={handlePay}
          disabled={disabled}
          style={{
            height: 44,
            width: "100%",
            background: isSuccess ? "var(--bg-elev-3)" : "var(--accent)",
            border: "1px solid var(--accent-line)",
            borderRadius: "var(--r-2)",
            color: isSuccess ? "var(--accent)" : "var(--accent-fg)",
            fontSize: 14,
            fontWeight: 600,
            cursor: disabled ? "default" : "pointer",
            opacity: disabled && !isSuccess ? 0.5 : 1,
            fontFamily: "var(--font-sans)",
            transition: "background 0.18s var(--ease)",
          }}
        >
          {buttonLabel}
        </button>

        {status === "idle" && (
          <p
            style={{
              margin: "10px 0 0",
              fontSize: 12,
              color: "var(--fg-dim)",
              textAlign: "center",
            }}
          >
            {!payable
              ? "Your cart is empty."
              : method === "card"
                ? "Enter valid card details to pay."
                : "Card payment is required to complete checkout."}
          </p>
        )}
      </div>
    </div>
  );
}
