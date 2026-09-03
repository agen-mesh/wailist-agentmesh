"use client";
import { useState } from "react";
import { Pill } from "@/components/ui";
import type { PaymentMethod } from "./types";
import { USD_PROVIDERS } from "./paymentProviders";
import { usePaymentProviders } from "./usePaymentProviders";
import { payments } from "@/lib/api";
import { useCashfreeCheckout } from "./useCashfreeCheckout";

type PayStatus = "idle" | "processing" | "success";

const PHONE_STORAGE_KEY = "agentmesh_checkout_phone";

// normalizePhone mirrors the backend's normalizeIndianPhone (payments.go) —
// strips formatting and an optional country code down to the bare 10 digits
// Cashfree expects. This copy only gates the Pay button early and shows a
// friendly hint; the server validates again and is the actual authority.
function normalizePhone(raw: string): string | null {
  let digits = raw.replace(/\D/g, "");
  digits = digits.replace(/^0/, "");
  if (digits.length === 12 && digits.startsWith("91")) digits = digits.slice(2);
  return /^[6-9]\d{9}$/.test(digits) ? digits : null;
}

const PANEL_CSS = `
.checkout-pay { transition: background 0.18s var(--ease), transform 0.12s var(--ease); }
.checkout-pay:not(:disabled):active { transform: scale(0.985); }
.checkout-provider { transition: border-color 0.15s var(--ease), background 0.15s var(--ease); }
/* The row is not clickable -- only the box inside it is -- so the pointer
   cursor and hover affordance belong to the box, not the whole strip. */
.checkout-agree { transition: border-color 0.15s var(--ease), background 0.15s var(--ease); }
.checkout-agree:focus-within { border-color: var(--accent-line) !important; }
.checkout-agree-box:hover span { border-color: var(--accent) !important; }
@media (prefers-reduced-motion: reduce) {
  .checkout-pay, .checkout-provider { transition: none; }
}
`;

// Right-hand payment column: pick a provider, then pay. Cashfree runs the real
// hosted-checkout flow (order → SDK modal → server-side verify). NOWPayments,
// PayPal and Stripe render disabled ("coming soon").
// The provider hosts card entry, so there is no in-app card form here.
export function PaymentInfoPanel({
  method,
  onMethodChange,
  amountINR,
  payable,
  onPaid,
}: {
  method: PaymentMethod;
  onMethodChange: (method: PaymentMethod) => void;
  amountINR: number;
  payable: boolean;
  onPaid: (creditsUSDOverride?: number) => void;
}) {
  const [status, setStatus] = useState<PayStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const [agreed, setAgreed] = useState(false);
  // Cashfree requires a real customer phone on every order — it used to be
  // sent as a hardcoded placeholder, which showed a fake number back to real
  // paying customers on their own receipt. Remembered across visits (a
  // contact number, not a secret) so returning to top up doesn't mean
  // retyping it.
  const [phone, setPhone] = useState(() =>
    typeof window === "undefined"
      ? ""
      : (window.localStorage.getItem(PHONE_STORAGE_KEY) ?? ""),
  );
  const [phoneTouched, setPhoneTouched] = useState(false);
  const normalizedPhone = normalizePhone(phone);
  const phoneValid = normalizedPhone !== null;

  const handlePhoneChange = (value: string) => {
    setPhone(value);
    if (typeof window !== "undefined") {
      const n = normalizePhone(value);
      if (n) window.localStorage.setItem(PHONE_STORAGE_KEY, value);
    }
  };

  const finish = (creditsUSDOverride?: number) => {
    setStatus("success");
    onPaid(creditsUSDOverride);
  };

  const cashfree = useCashfreeCheckout({
    onSuccess: (creditedUsdMicros) => finish(creditedUsdMicros / 1e6),
    onError: (msg) => {
      setError(msg);
      setStatus("idle");
    },
    onDismiss: () => setStatus("idle"),
  });

  const { providers, usdPerINR, loading: providersLoading } =
    usePaymentProviders();
  const selected = providers.find((p) => p.id === method);

  // The USD gateways charge in dollars while this panel is denominated in
  // rupees, so the amount is converted at the server's live rate -- never at
  // lib/credits/fx.ts's mock constant, which would quote a price that differs
  // from what actually gets charged.
  const amountUSDCents =
    usdPerINR > 0 ? Math.round(amountINR * usdPerINR * 100) : 0;
  const isUSD = USD_PROVIDERS.has(method);
  const busy = status === "processing" || cashfree.loading || providersLoading;
  const isSuccess = status === "success";
  // Cashfree specifically needs a real phone; other providers don't ask for
  // one, so this gate only applies when that method is selected.
  const canPay =
    !!selected?.enabled &&
    payable &&
    !busy &&
    !isSuccess &&
    agreed &&
    (method !== "cashfree" || phoneValid) &&
    // A USD gateway with no rate cannot be priced, so it cannot be paid.
    (!isUSD || amountUSDCents > 0);

  // Stripe, PayPal and NOWPayments are all redirect flows: the browser leaves
  // for the provider's hosted page and returns to /billing, where the query
  // param is picked up and the payment finalised. Only Cashfree completes
  // in-page, via its own JS SDK.
  const handlePay = async () => {
    if (!canPay) return;
    setError(null);

    if (method === "cashfree") {
      if (!normalizedPhone) {
        setPhoneTouched(true);
        return;
      }
      // useCashfreeCheckout owns loading/dismiss; leave status idle so the
      // button reflects the hook's state, not a stuck "processing".
      cashfree.pay(Math.round(amountINR * 100), normalizedPhone);
      return;
    }

    setStatus("processing");
    try {
      let redirectTo: string;
      if (method === "stripe") {
        const res = await payments.createStripeCheckout(amountUSDCents);
        // The session id has to survive the round trip: the return URL only
        // carries our order id, and verify needs both.
        window.sessionStorage.setItem("agentmesh_stripe_session", res.session_id);
        redirectTo = res.checkout_url;
      } else if (method === "paypal") {
        const res = await payments.createPayPalOrder(amountUSDCents);
        window.sessionStorage.setItem("agentmesh_paypal_order", res.paypal_order_id);
        redirectTo = res.approve_url;
      } else {
        const res = await payments.createCryptoInvoice(amountUSDCents);
        redirectTo = res.invoice_url;
      }
      window.location.assign(redirectTo);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "could not start checkout");
      setStatus("idle");
    }
  };

  const buttonLabel = isSuccess
    ? "✓ Payment successful"
    : busy
      ? "Processing…"
      : !payable
        ? "Add an amount to continue"
        : isUSD
          ? `Pay $${(amountUSDCents / 100).toFixed(2)}`
          : `Pay ₹${amountINR.toFixed(2)}`;

  const trust = `Secured by ${selected?.label ?? "our payment provider"} · details are encrypted`;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
      <style>{PANEL_CSS}</style>
      <div style={{ fontSize: 16, fontWeight: 600, color: "var(--fg)" }}>
        Payment method
      </div>

      {/* Provider selector */}
      <div
        role="radiogroup"
        aria-label="Payment method"
        style={{ display: "flex", flexDirection: "column", gap: 10 }}
      >
        {providers.map((p) => {
          const active = method === p.id;
          const disabled = !p.enabled;
          return (
            <button
              key={p.id}
              type="button"
              className="checkout-provider"
              role="radio"
              aria-checked={active}
              aria-disabled={disabled}
              disabled={disabled}
              onClick={() => !disabled && onMethodChange(p.id)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                width: "100%",
                textAlign: "left",
                padding: "12px 14px",
                borderRadius: "var(--r-2)",
                border: `1px solid ${active ? "var(--accent-line)" : "var(--border)"}`,
                background: active ? "var(--accent-soft)" : "var(--bg)",
                color: disabled ? "var(--fg-dim)" : "var(--fg)",
                cursor: disabled ? "not-allowed" : "pointer",
                opacity: disabled ? 0.55 : 1,
                fontFamily: "var(--font-sans)",
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 16,
                  height: 16,
                  borderRadius: 999,
                  flexShrink: 0,
                  border: `1px solid ${active ? "var(--accent)" : "var(--border-strong)"}`,
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                }}
              >
                {active && (
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
              <span
                style={{ display: "flex", flexDirection: "column", gap: 2 }}
              >
                <span style={{ fontSize: 13, fontWeight: 500 }}>{p.label}</span>
                <span style={{ fontSize: 11, color: "var(--fg-dim)" }}>
                  {p.sublabel}
                </span>
              </span>
              {disabled && (
                <span style={{ marginLeft: "auto" }}>
                  <Pill>Soon</Pill>
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Cashfree needs a real contact number on the order — shown only for
          that provider, since others don't ask for one. */}
      {method === "cashfree" && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <label
            htmlFor="checkout-phone"
            style={{ fontSize: 12, fontWeight: 500, color: "var(--fg-muted)" }}
          >
            Phone number
          </label>
          <input
            id="checkout-phone"
            type="tel"
            inputMode="numeric"
            autoComplete="tel"
            placeholder="98765 43210"
            value={phone}
            onChange={(e) => handlePhoneChange(e.target.value)}
            onBlur={() => setPhoneTouched(true)}
            aria-invalid={phoneTouched && !phoneValid}
            style={{
              height: 40,
              padding: "0 12px",
              borderRadius: "var(--r-2)",
              border: `1px solid ${
                phoneTouched && !phoneValid ? "var(--danger)" : "var(--border)"
              }`,
              background: "var(--bg)",
              color: "var(--fg)",
              fontSize: 13,
              fontFamily: "var(--font-mono)",
            }}
          />
          <span
            style={{
              fontSize: 11,
              color:
                phoneTouched && !phoneValid ? "var(--danger)" : "var(--fg-dim)",
            }}
          >
            {phoneTouched && !phoneValid
              ? "Enter a valid 10-digit mobile number."
              : "Cashfree sends payment confirmations here."}
          </span>
        </div>
      )}

      <div style={{ marginTop: "auto" }}>
        {/* Trust signal */}
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
          {trust}
        </div>

        <button
          type="button"
          className="checkout-pay"
          onClick={handlePay}
          disabled={!canPay}
          style={{
            height: 44,
            width: "100%",
            background: isSuccess ? "var(--bg-elev-3)" : "var(--accent)",
            border: "1px solid var(--accent-line)",
            borderRadius: "var(--r-2)",
            color: isSuccess ? "var(--accent)" : "var(--accent-fg)",
            fontSize: 14,
            fontWeight: 600,
            cursor: canPay ? "pointer" : "default",
            opacity: !canPay && !isSuccess ? 0.5 : 1,
            fontFamily: "var(--font-sans)",
          }}
        >
          {buttonLabel}
        </button>

        {/* ── Compliance checkbox ── */}
        {!isSuccess && (
          <div
            className="checkout-agree"
            style={{
              display: "flex",
              alignItems: "flex-start",
              gap: 9,
              marginTop: 12,
              padding: "10px 12px",
              borderRadius: "var(--r-2)",
              border: `1px solid ${agreed ? "var(--border)" : "var(--border-soft)"}`,
              background: agreed ? "var(--bg-elev-1)" : "transparent",
            }}
          >
            {/* Only the box toggles. The <label> wraps the box alone rather than
                the whole row, so clicking (or dragging across) the disclosure
                text can't flip an agreement the user was only trying to read. */}
            <label
              className="checkout-agree-box"
              style={{
                display: "inline-flex",
                flexShrink: 0,
                marginTop: 1,
                cursor: "pointer",
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 15,
                  height: 15,
                  borderRadius: 4,
                  border: `1.5px solid ${agreed ? "var(--accent)" : "var(--border-strong)"}`,
                  background: agreed ? "var(--accent)" : "transparent",
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  transition: "background 0.15s, border-color 0.15s",
                }}
              >
                {agreed && (
                  <svg
                    width="9"
                    height="7"
                    viewBox="0 0 9 7"
                    fill="none"
                    aria-hidden="true"
                  >
                    <path
                      d="M1 3.5L3.5 6L8 1"
                      stroke="var(--accent-fg)"
                      strokeWidth="1.6"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                )}
              </span>
              <input
                type="checkbox"
                checked={agreed}
                onChange={(e) => setAgreed(e.target.checked)}
                style={{
                  position: "absolute",
                  opacity: 0,
                  width: 0,
                  height: 0,
                  pointerEvents: "none",
                }}
                aria-label="I understand that AgentMesh credits are non-refundable and hold no monetary value"
              />
            </label>
            {/* Not selectable: dragging over this text used to leave a
                highlighted word mid-sentence. The Terms/Refund Policy links
                stay clickable. */}
            <span
              style={{
                fontSize: 11.5,
                lineHeight: 1.6,
                color: "var(--fg-muted)",
                userSelect: "none",
                WebkitUserSelect: "none",
              }}
            >
              I understand that AgentMesh credits are{" "}
              <strong style={{ color: "var(--fg)", fontWeight: 600 }}>
                non-refundable
              </strong>{" "}
              once purchased and hold{" "}
              <strong style={{ color: "var(--fg)", fontWeight: 600 }}>
                no monetary or cash value
              </strong>
              . By proceeding I agree to the{" "}
              <a
                href="/terms"
                target="_blank"
                rel="noreferrer"
                onClick={(e) => e.stopPropagation()}
                style={{ color: "var(--accent)", textDecoration: "none" }}
              >
                Terms
              </a>{" "}
              &amp;{" "}
              <a
                href="/refund-policy"
                target="_blank"
                rel="noreferrer"
                onClick={(e) => e.stopPropagation()}
                style={{ color: "var(--accent)", textDecoration: "none" }}
              >
                Refund Policy
              </a>
              .
            </span>
          </div>
        )}

        {error && (
          <p
            style={{
              margin: "10px 0 0",
              fontSize: 12,
              color: "var(--danger)",
              textAlign: "center",
            }}
          >
            {error}
          </p>
        )}

        {!error && !busy && !isSuccess && (
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
              : method === "cashfree" && !phoneValid
                ? "Enter your phone number above to continue."
                : !agreed
                  ? "Please confirm the credit policy above to continue."
                  : `You'll be redirected to ${selected?.label ?? "the provider"} to complete payment.`}
          </p>
        )}
      </div>
    </div>
  );
}
