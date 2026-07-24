"use client";
import { useState } from "react";
import { IconArrow, IconWallet } from "@/components/ui";
import { PurchaseHistory } from "@/components/billing/PurchaseHistory";
import { AutoRechargeSettings } from "@/components/billing/AutoRechargeSettings";
import { CheckoutModal } from "@/components/checkout/CheckoutModal";
import { useCredits } from "@/lib/credits/store";
import { bonusRate, creditsForTopup } from "@/lib/credits/fx";

const PRESETS_INR = [100, 500, 1000, 2000];
const LOW_BALANCE_USD = 5;

const BILLING_CSS = `
.bill-reveal { animation: fade-up 0.45s var(--ease) both; }
.bill-preset { transition: transform 0.15s var(--ease), border-color 0.15s var(--ease), background 0.15s var(--ease); }
.bill-preset:hover { transform: translateY(-2px); border-color: var(--border-strong); }
.bill-cta { transition: transform 0.12s var(--ease), box-shadow 0.2s var(--ease); }
.bill-cta:not(:disabled):hover { box-shadow: 0 12px 34px var(--accent-glow); }
.bill-cta:not(:disabled):active { transform: scale(0.99); }
@media (prefers-reduced-motion: reduce) {
  .bill-reveal, .bill-preset, .bill-cta { animation: none; transition: none; }
}
`;

const fmtUSD = (n: number) => `$${n.toFixed(2)}`;

export default function BillingPage() {
  const [amountINR, setAmountINR] = useState<number>(PRESETS_INR[1]);
  const [customINR, setCustomINR] = useState("");
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const { balanceUSD, lastPurchase } = useCredits();

  const openCheckoutFor = (inr: number) => {
    setCustomINR(String(inr));
    setCheckoutOpen(true);
  };

  const parsedCustom = customINR ? parseFloat(customINR) : NaN;
  const effectiveINR = customINR
    ? Number.isFinite(parsedCustom)
      ? parsedCustom
      : 0
    : amountINR;
  const checkoutAmountINR = effectiveINR >= 1 ? effectiveINR : 0;
  const canCheckout = checkoutAmountINR > 0;
  const credits = creditsForTopup(checkoutAmountINR);
  const isLow = balanceUSD < LOW_BALANCE_USD;

  return (
    <div style={{ maxWidth: 560, margin: "56px auto 96px", padding: "0 24px" }}>
      <style>{BILLING_CSS}</style>

      {/* Header */}
      <div className="bill-reveal" style={{ marginBottom: 20 }}>
        <h1
          style={{
            fontSize: 26,
            fontWeight: 700,
            letterSpacing: "-0.02em",
            margin: 0,
            color: "var(--fg)",
          }}
        >
          Add credits
        </h1>
        <p
          style={{
            margin: "6px 0 0",
            fontSize: 14,
            color: "var(--fg-muted)",
            lineHeight: 1.5,
          }}
        >
          Credits are spent as your agents call paid tools and models. Top up
          anytime — testnet usage stays free.
        </p>
      </div>

      {/* Balance hero */}
      <div
        className="bill-reveal"
        style={{
          animationDelay: "0.05s",
          position: "relative",
          overflow: "hidden",
          background:
            "linear-gradient(135deg, var(--bg-elev-2), var(--bg-elev-1))",
          border: "1px solid var(--border)",
          borderRadius: "var(--r-3)",
          padding: 20,
          marginBottom: 20,
        }}
      >
        {/* accent bloom */}
        <div
          aria-hidden
          style={{
            position: "absolute",
            top: -60,
            right: -40,
            width: 200,
            height: 200,
            borderRadius: 999,
            background: "var(--accent-glow)",
            filter: "blur(60px)",
            opacity: 0.5,
            pointerEvents: "none",
          }}
        />
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            position: "relative",
          }}
        >
          <div>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 7,
                color: "var(--fg-muted)",
                fontSize: 12,
                fontWeight: 500,
              }}
            >
              <IconWallet size={14} /> Wallet balance
            </div>
            <div
              style={{
                marginTop: 8,
                fontFamily: "var(--font-mono)",
                fontSize: 34,
                fontWeight: 600,
                letterSpacing: "-0.01em",
                color: "var(--fg)",
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {fmtUSD(balanceUSD)}
            </div>
          </div>
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
              height: 24,
              padding: "0 10px",
              borderRadius: 999,
              fontSize: 11,
              fontWeight: 500,
              border: `1px solid ${isLow ? "rgba(255,181,71,0.35)" : "var(--accent-line)"}`,
              background: isLow ? "var(--warm-soft)" : "var(--accent-soft)",
              color: isLow ? "var(--warm)" : "var(--accent)",
            }}
          >
            <span
              style={{
                width: 6,
                height: 6,
                borderRadius: 999,
                background: isLow ? "var(--warm)" : "var(--accent)",
              }}
            />
            {isLow ? "Low balance" : "Active"}
          </span>
        </div>
      </div>

      {/* Top-up panel */}
      <div
        className="bill-reveal"
        style={{
          animationDelay: "0.1s",
          background: "var(--bg-elev-1)",
          border: "1px solid var(--border)",
          borderRadius: "var(--r-3)",
          padding: 20,
          marginBottom: 24,
        }}
      >
        <div
          style={{
            fontSize: 12,
            fontWeight: 600,
            color: "var(--fg-muted)",
            marginBottom: 12,
          }}
        >
          Choose an amount
        </div>

        {/* Preset cards */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(4, 1fr)",
            gap: 8,
          }}
        >
          {PRESETS_INR.map((inr) => {
            const selected = !customINR && amountINR === inr;
            const hasBonus = bonusRate(inr) > 0;
            return (
              <button
                key={inr}
                type="button"
                className="bill-preset"
                onClick={() => {
                  setAmountINR(inr);
                  setCustomINR("");
                }}
                style={{
                  position: "relative",
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "flex-start",
                  gap: 3,
                  padding: "12px 12px 11px",
                  borderRadius: "var(--r-2)",
                  border: `1px solid ${selected ? "var(--accent)" : "var(--border)"}`,
                  background: selected ? "var(--accent-soft)" : "var(--bg)",
                  cursor: "pointer",
                  boxShadow: selected ? "0 0 0 3px var(--accent-soft)" : "none",
                  fontFamily: "var(--font-sans)",
                }}
              >
                <span
                  style={{
                    fontSize: 16,
                    fontWeight: 700,
                    color: "var(--fg)",
                    letterSpacing: "-0.01em",
                  }}
                >
                  ₹{inr}
                </span>
                <span
                  style={{
                    fontSize: 11,
                    color: "var(--fg-dim)",
                    fontFamily: "var(--font-mono)",
                    fontVariantNumeric: "tabular-nums",
                  }}
                >
                  ≈ {fmtUSD(creditsForTopup(inr))}
                </span>
                {hasBonus && (
                  <span
                    style={{
                      position: "absolute",
                      top: 8,
                      right: 8,
                      fontSize: 9,
                      fontWeight: 700,
                      color: "var(--accent)",
                      background: "var(--accent-soft)",
                      border: "1px solid var(--accent-line)",
                      borderRadius: 999,
                      padding: "1px 5px",
                    }}
                  >
                    +5%
                  </span>
                )}
              </button>
            );
          })}
        </div>

        {/* Custom amount */}
        <div style={{ marginTop: 14 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              height: 42,
              padding: "0 12px",
              borderRadius: "var(--r-2)",
              border: `1px solid ${customINR ? "var(--accent-line)" : "var(--border)"}`,
              background: "var(--bg)",
            }}
          >
            <span style={{ color: "var(--fg-muted)", fontSize: 15 }}>₹</span>
            <input
              type="number"
              inputMode="numeric"
              placeholder="Custom amount"
              value={customINR}
              onChange={(e) => setCustomINR(e.target.value)}
              style={{
                flex: 1,
                height: "100%",
                background: "transparent",
                border: "none",
                outline: "none",
                color: "var(--fg)",
                fontSize: 14,
                fontFamily: "var(--font-sans)",
              }}
            />
            {canCheckout && (
              <span
                style={{
                  fontSize: 12,
                  color: "var(--fg-muted)",
                  fontFamily: "var(--font-mono)",
                  fontVariantNumeric: "tabular-nums",
                  whiteSpace: "nowrap",
                }}
              >
                ≈ {fmtUSD(credits)} credits
              </span>
            )}
          </div>
          <p
            style={{
              margin: "8px 2px 0",
              fontSize: 11,
              color: "var(--fg-dim)",
            }}
          >
            Get 5% bonus credits on top-ups of ₹1000 or more.
          </p>
        </div>

        {lastPurchase && (
          <button
            type="button"
            onClick={() => openCheckoutFor(lastPurchase.amountINR)}
            style={{
              width: "100%",
              height: 36,
              marginTop: 14,
              borderRadius: "var(--r-2)",
              border: "1px solid var(--accent-line)",
              background: "var(--accent-soft)",
              color: "var(--accent)",
              fontSize: 12.5,
              fontWeight: 500,
              cursor: "pointer",
            }}
          >
            ↻ Repeat last top-up · ₹{lastPurchase.amountINR}
          </button>
        )}

        {/* Primary CTA */}
        <button
          type="button"
          className="bill-cta"
          onClick={() => setCheckoutOpen(true)}
          disabled={!canCheckout}
          style={{
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            gap: 8,
            width: "100%",
            height: 46,
            marginTop: 14,
            borderRadius: "var(--r-2)",
            border: "1px solid var(--accent-line)",
            background: canCheckout
              ? "linear-gradient(180deg, var(--accent), var(--accent-strong))"
              : "var(--bg-elev-2)",
            color: canCheckout ? "var(--accent-fg)" : "var(--fg-dim)",
            fontSize: 14,
            fontWeight: 600,
            cursor: canCheckout ? "pointer" : "default",
            boxShadow: canCheckout ? "0 8px 24px var(--accent-glow)" : "none",
            fontFamily: "var(--font-sans)",
          }}
        >
          {canCheckout ? (
            <>
              Continue to checkout · ₹{checkoutAmountINR.toFixed(2)}
              <IconArrow size={13} />
            </>
          ) : (
            "Enter an amount of ₹1 or more"
          )}
        </button>
      </div>

      {/* History + auto-recharge */}
      <div className="bill-reveal" style={{ animationDelay: "0.15s" }}>
        <PurchaseHistory onBuyAgain={openCheckoutFor} />
        <AutoRechargeSettings />
      </div>

      {checkoutOpen && (
        <CheckoutModal
          open
          amountINR={checkoutAmountINR}
          onClose={() => setCheckoutOpen(false)}
        />
      )}
    </div>
  );
}
