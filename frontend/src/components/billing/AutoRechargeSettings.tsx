"use client";
import { type CSSProperties } from "react";
import { Card } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";
import type { AutoRecharge } from "@/lib/credits/types";

const fieldStyle: CSSProperties = {
  width: "100%",
  height: 34,
  padding: "0 10px",
  background: "var(--bg)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  color: "var(--fg)",
  fontSize: 13,
  fontFamily: "var(--font-sans)",
  outline: "none",
};

const labelStyle: CSSProperties = {
  display: "block",
  fontSize: 12,
  color: "var(--fg-muted)",
  marginBottom: 6,
};

// Auto-recharge configuration for the mock wallet. The settings persist (via the
// store), but no real recharge is ever triggered — this is a control surface
// only until a billing backend exists.
export function AutoRechargeSettings() {
  const { autoRecharge, setAutoRecharge, hydrated } = useCredits();

  if (!hydrated) return null;

  const update = (patch: Partial<AutoRecharge>) =>
    setAutoRecharge({ ...autoRecharge, ...patch });

  const { enabled, thresholdUSD, amountINR, monthlyCapINR } = autoRecharge;

  return (
    <div style={{ marginTop: 32 }}>
      <h2
        style={{
          fontSize: 15,
          fontWeight: 600,
          color: "var(--fg)",
          marginBottom: 12,
        }}
      >
        Auto-recharge
      </h2>

      <Card style={{ display: "flex", flexDirection: "column", gap: 16 }}>
        <label
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 12,
            cursor: "pointer",
          }}
        >
          <span style={{ fontSize: 13, color: "var(--fg)" }}>
            Automatically top up when my balance is low
          </span>
          <button
            type="button"
            role="switch"
            aria-checked={enabled}
            aria-label="Enable auto-recharge"
            onClick={() => update({ enabled: !enabled })}
            style={{
              flexShrink: 0,
              width: 40,
              height: 22,
              padding: 2,
              borderRadius: 999,
              border: "1px solid var(--border-strong)",
              background: enabled ? "var(--accent)" : "var(--bg-elev-2)",
              cursor: "pointer",
              display: "inline-flex",
              justifyContent: enabled ? "flex-end" : "flex-start",
              transition: "background 0.15s var(--ease)",
            }}
          >
            <span
              style={{
                width: 16,
                height: 16,
                borderRadius: 999,
                background: enabled ? "var(--accent-fg)" : "var(--fg-dim)",
              }}
            />
          </button>
        </label>

        {enabled && (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 12,
            }}
          >
            <div>
              <label style={labelStyle} htmlFor="ar-threshold">
                When balance falls below (USD)
              </label>
              <input
                id="ar-threshold"
                type="number"
                min={0}
                value={thresholdUSD}
                onChange={(e) =>
                  update({ thresholdUSD: Math.max(0, Number(e.target.value)) })
                }
                style={fieldStyle}
              />
            </div>
            <div>
              <label style={labelStyle} htmlFor="ar-amount">
                Top up amount (INR)
              </label>
              <input
                id="ar-amount"
                type="number"
                min={0}
                value={amountINR}
                onChange={(e) =>
                  update({ amountINR: Math.max(0, Number(e.target.value)) })
                }
                style={fieldStyle}
              />
            </div>
            <div style={{ gridColumn: "1 / -1" }}>
              <label style={labelStyle} htmlFor="ar-cap">
                Monthly cap (INR, optional)
              </label>
              <input
                id="ar-cap"
                type="number"
                min={0}
                placeholder="No limit"
                value={monthlyCapINR ?? ""}
                onChange={(e) =>
                  update({
                    monthlyCapINR:
                      e.target.value === ""
                        ? null
                        : Math.max(0, Number(e.target.value)),
                  })
                }
                style={fieldStyle}
              />
            </div>
          </div>
        )}

        <p style={{ fontSize: 11, color: "var(--fg-dim)", margin: 0 }}>
          Mock setting — no real recharge is triggered.
        </p>
      </Card>
    </div>
  );
}
