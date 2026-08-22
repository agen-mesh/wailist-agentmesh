"use client";
import { useState } from "react";
import type { UserSettings, UserSettingsPatch } from "@/lib/api";
import {
  FormStatus,
  SaveButton,
  SettingRow,
  SettingsSection,
  amountInputStyle,
} from "@/components/settings/ui";
import { useCurrency } from "@/lib/currency/store";

type SaveState = "idle" | "saving" | "saved" | "error";

// Mirror models.MaxSingleX402QuoteUSDMicros and models.X402ProbeFloorUSDMicros.
// The server is authoritative and rejects anything outside this range; these
// exist so the form can say so before a round trip, not as the only check.
const PLATFORM_CEILING_USD = 1000;
// Below this a ceiling stops limiting spend and starts blocking tool402 nodes
// outright — the engine checks the same floor before a tool's price is known.
const MIN_CEILING_USD = 0.05;

const microsToUSD = (micros: number): string => String(micros / 1e6);
const usdToMicros = (usd: number): number => Math.round(usd * 1e6);

export function ExecutionSection({
  settings,
  onSave,
}: {
  settings: UserSettings;
  onSave: (patch: UserSettingsPatch) => Promise<void>;
}) {
  const stored = settings.maxCallSpendUsdMicros;
  const [limited, setLimited] = useState(stored != null);
  const [ceiling, setCeiling] = useState(
    stored != null ? microsToUSD(stored) : "1",
  );
  const [state, setState] = useState<SaveState>("idle");
  const [message, setMessage] = useState("");
  const { format: formatMoney, isDefault: isUSD } = useCurrency();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();

    // null is the only way to remove a ceiling — see UserSettingsPatch.
    let maxCallSpendUsdMicros: number | null = null;
    if (limited) {
      const parsed = Number(ceiling);
      if (!Number.isFinite(parsed) || parsed <= 0) {
        setState("error");
        setMessage("Enter a limit greater than zero.");
        return;
      }
      if (parsed < MIN_CEILING_USD) {
        setState("error");
        setMessage(
          `The lowest usable limit is $${MIN_CEILING_USD.toFixed(2)} — below that, paid tool calls are blocked outright rather than limited.`,
        );
        return;
      }
      if (parsed > PLATFORM_CEILING_USD) {
        setState("error");
        setMessage(
          `The platform ceiling is $${PLATFORM_CEILING_USD} per call.`,
        );
        return;
      }
      maxCallSpendUsdMicros = usdToMicros(parsed);
    }

    setState("saving");
    try {
      await onSave({ maxCallSpendUsdMicros });
      setState("saved");
      setMessage("Execution settings saved.");
    } catch (err) {
      setState("error");
      setMessage(err instanceof Error ? err.message : "Could not save.");
    }
  };

  return (
    <SettingsSection
      id="execution"
      title="Execution and safety"
      description="Guardrails applied to every run. An agent decides what to call on its own, so these are the limits it cannot talk its way past."
    >
      <form onSubmit={submit} style={{ display: "grid", gap: 18 }}>
        <SettingRow
          label="Per-call spend limit"
          hint={`Refuse any single paid call above this amount, before it runs. Anywhere from $${MIN_CEILING_USD.toFixed(2)} to $${PLATFORM_CEILING_USD}. Without one, only the platform ceiling of $${PLATFORM_CEILING_USD} per call applies.`}
        >
          <label
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              fontSize: 13,
              color: "var(--fg-muted)",
              cursor: "pointer",
              // Keeps the tap target past the 44px minimum even though the
              // checkbox itself is small.
              minHeight: 44,
            }}
          >
            <input
              type="checkbox"
              checked={limited}
              onChange={(e) => setLimited(e.target.checked)}
              style={{ width: 16, height: 16, accentColor: "var(--accent)" }}
            />
            Set a limit for this account
          </label>
          {limited && (
            <>
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  maxWidth: 220,
                }}
              >
                <span style={{ fontSize: 13, color: "var(--fg-muted)" }}>
                  $
                </span>
                <input
                  id="set-call-ceiling"
                  inputMode="decimal"
                  value={ceiling}
                  onChange={(e) => setCeiling(e.target.value)}
                  aria-label="Per-call spend limit in USD"
                  style={amountInputStyle}
                />
              </div>
              {/* The field stays USD even in another display currency, because
                  the engine enforces this limit in USD micros. Accepting euros
                  here would mean converting on save and back on load, so the
                  number would drift by the day's rate every time it was
                  reopened. A read-only preview is honest and stable. */}
              {!isUSD && Number.isFinite(Number(ceiling)) && (
                <p
                  style={{
                    margin: 0,
                    fontSize: 12,
                    color: "var(--fg-dim)",
                    fontFamily: "var(--font-mono)",
                  }}
                >
                  ≈ {formatMoney(Number(ceiling))} at today&apos;s rate ·
                  enforced in USD
                </p>
              )}
            </>
          )}
        </SettingRow>

        <SaveButton saving={state === "saving"} />
        <FormStatus state={state} message={message} />
      </form>
    </SettingsSection>
  );
}
