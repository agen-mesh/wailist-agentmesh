"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import type { UserSettings, UserSettingsPatch } from "@/lib/api";
import { useCredits } from "@/lib/credits/store";
import {
  CURRENCY_LABELS,
  SUPPORTED_CURRENCIES,
  isSupportedCurrency,
} from "@/lib/currency/format";
import { applyDisplayCurrency, useCurrency } from "@/lib/currency/store";
import {
  FormStatus,
  SaveButton,
  SettingRow,
  SettingsSection,
  amountInputStyle,
  inputStyle,
} from "@/components/settings/ui";
import { ghostBtnSm } from "@/components/ui";

type SaveState = "idle" | "saving" | "saved" | "error";

// Micros are the storage unit everywhere in this codebase; dollars exist only
// for display. Rounding on the way in keeps a typed "12.3456789" from becoming
// a fractional micro the BIGINT column can't hold.
const microsToUSD = (micros: number): string => (micros / 1e6).toFixed(2);
const usdToMicros = (usd: number): number => Math.round(usd * 1e6);

export function BillingSection({
  settings,
  onSave,
}: {
  settings: UserSettings;
  onSave: (patch: UserSettingsPatch) => Promise<void>;
}) {
  const router = useRouter();
  const { balanceUSD, balanceKnown, refreshBalance } = useCredits();
  const { formatBalance, isDefault: isUSD, ratesFailed } = useCurrency();
  const [threshold, setThreshold] = useState(
    microsToUSD(settings.lowBalanceUsdMicros),
  );
  const [state, setState] = useState<SaveState>("idle");
  const [message, setMessage] = useState("");
  const [currencyState, setCurrencyState] = useState<SaveState>("idle");
  const [currencyMessage, setCurrencyMessage] = useState("");

  // Saved on change rather than behind a button: it is a single-choice display
  // preference with an immediately visible effect, so a "Save" step would just
  // be a second click before seeing what you picked.
  const changeCurrency = async (code: string) => {
    if (!isSupportedCurrency(code)) return;
    setCurrencyState("saving");
    try {
      await onSave({ displayCurrency: code });
      // Only after the server has stored it. useAuth caches the user from
      // mount, so this is what makes the change visible without a reload.
      applyDisplayCurrency(code);
      setCurrencyState("saved");
      setCurrencyMessage(
        code === "USD"
          ? "Showing amounts in US dollars."
          : `Showing amounts in ${CURRENCY_LABELS[code]}.`,
      );
    } catch (err) {
      setCurrencyState("error");
      setCurrencyMessage(
        err instanceof Error ? err.message : "Could not change currency.",
      );
    }
  };

  // The store only holds a balance once something has fetched one, and it is
  // deliberately never restored from localStorage (a cached balance goes stale
  // the moment a run spends credits). Without this the panel shows an em dash
  // forever — the credits page does the same on mount for the same reason.
  useEffect(() => {
    void refreshBalance();
  }, [refreshBalance]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const parsed = Number(threshold);
    if (!Number.isFinite(parsed) || parsed < 0) {
      setState("error");
      setMessage("Enter a threshold of zero or more.");
      return;
    }
    setState("saving");
    try {
      await onSave({ lowBalanceUsdMicros: usdToMicros(parsed) });
      setState("saved");
      setMessage("Low balance threshold saved.");
    } catch (err) {
      setState("error");
      setMessage(err instanceof Error ? err.message : "Could not save.");
    }
  };

  return (
    <SettingsSection
      id="billing"
      title="Billing and credits"
      description="Credits are spent as your agents call paid tools, x402 endpoints, and LLM providers."
    >
      <SettingRow
        label="Display currency"
        htmlFor="set-currency"
        hint="Changes how amounts are shown across the app. It does not change what you are charged — top-ups are always billed in INR, and credits are always spent in USD."
      >
        <select
          id="set-currency"
          value={settings.displayCurrency}
          onChange={(e) => changeCurrency(e.target.value)}
          disabled={currencyState === "saving"}
          style={{ ...inputStyle, maxWidth: 260 }}
        >
          {SUPPORTED_CURRENCIES.map((code) => (
            <option key={code} value={code}>
              {code} · {CURRENCY_LABELS[code]}
            </option>
          ))}
        </select>
        <FormStatus state={currencyState} message={currencyMessage} />
        {/* The one place the fallback is admitted to the user. Amounts quietly
            render in USD when the rate table can't be fetched — silently
            showing dollars to someone who asked for euros looks like a bug,
            and a converted figure from an unknown rate would be worse. */}
        {ratesFailed && (
          <p
            role="status"
            style={{
              margin: 0,
              maxWidth: "60ch",
              fontSize: 12,
              lineHeight: 1.5,
              color: "var(--warm)",
            }}
          >
            Exchange rates are unavailable right now, so amounts are shown in US
            dollars. Your currency preference is saved and will apply once rates
            are reachable again.
          </p>
        )}
      </SettingRow>

      <div style={{ display: "grid", gap: 4 }}>
        <span style={{ fontSize: 12.5, fontWeight: 500, color: "var(--fg)" }}>
          Current balance
        </span>
        <span
          style={{
            fontSize: 22,
            fontWeight: 700,
            color: "var(--fg)",
            fontFamily: "var(--font-mono)",
            fontVariantNumeric: "tabular-nums",
            letterSpacing: "-0.01em",
          }}
        >
          {balanceKnown ? formatBalance(balanceUSD) : "—"}
        </span>
        {/* Shown only once a fiat estimate is on screen. The terms say credits
            are not currency and the refund policy says they cannot be exchanged
            for cash, so the converted figure must not read as a cash value.
            On USD there is no estimate and therefore nothing to qualify. */}
        {!isUSD && balanceKnown && (
          <span style={{ fontSize: 11.5, color: "var(--fg-dim)" }}>
            Converted at today&apos;s rate. Credits are not redeemable for cash.
          </span>
        )}
      </div>

      <form onSubmit={submit} style={{ display: "grid", gap: 18 }}>
        <SettingRow
          label="Low balance warning"
          htmlFor="set-low-balance"
          hint="Warn me when my balance drops below this. Drives the banner on the credits page and the low-balance indicator on the canvas."
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              maxWidth: 220,
            }}
          >
            <span style={{ fontSize: 13, color: "var(--fg-muted)" }}>$</span>
            <input
              id="set-low-balance"
              inputMode="decimal"
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              style={amountInputStyle}
            />
          </div>
        </SettingRow>
        <SaveButton saving={state === "saving"} />
        <FormStatus state={state} message={message} />
      </form>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        <button
          type="button"
          style={ghostBtnSm}
          onClick={() => router.push("/billing")}
        >
          Add credits
        </button>
        <button
          type="button"
          style={ghostBtnSm}
          onClick={() => router.push("/usage")}
        >
          View usage
        </button>
        <button
          type="button"
          style={ghostBtnSm}
          onClick={() => router.push("/refund-policy")}
        >
          Refund policy
        </button>
      </div>
    </SettingsSection>
  );
}
