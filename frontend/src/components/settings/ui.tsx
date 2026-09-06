"use client";
import { useCallback, useState } from "react";
import type { CSSProperties, ReactNode } from "react";

// Shared building blocks for the settings sections. Kept together so every
// section inherits the same panel surface, label hierarchy, and control sizing
// instead of each one re-deriving them from the tokens by hand.

// Matches app/billing/page.tsx's panelStyle — the settings panels sit at the
// same elevation as the billing ones because they play the same role.
export const panelStyle: CSSProperties = {
  background: "var(--bg-elev-1)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-3)",
  padding: 20,
};

export const inputStyle: CSSProperties = {
  height: 36,
  width: "100%",
  padding: "0 10px",
  background: "var(--bg-elev-2)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  color: "var(--fg)",
  fontSize: 13,
  fontFamily: "var(--font-sans)",
};

// Amounts line up in a column, so they take the mono face the rest of the app
// already uses for money (see billing/usage) rather than jiggling per digit.
export const amountInputStyle: CSSProperties = {
  ...inputStyle,
  fontFamily: "var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
};

export function SettingsSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-heading`}
      className="set-section"
      style={{ ...panelStyle, scrollMarginTop: 24 }}
    >
      <h2
        id={`${id}-heading`}
        style={{
          fontSize: 15,
          fontWeight: 600,
          margin: 0,
          color: "var(--fg)",
          letterSpacing: "-0.01em",
        }}
      >
        {title}
      </h2>
      {description && (
        <p
          style={{
            // ~65 characters keeps explanatory copy readable rather than
            // running the full panel width.
            maxWidth: "60ch",
            fontSize: 12.5,
            lineHeight: 1.55,
            color: "var(--fg-muted)",
            margin: "6px 0 0",
          }}
        >
          {description}
        </p>
      )}
      <div style={{ marginTop: 18, display: "grid", gap: 18 }}>{children}</div>
    </section>
  );
}

// One labelled control. `hint` explains what the setting actually does — every
// setting on this page changes real behaviour, and the hint is where that gets
// said plainly.
export function SettingRow({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div style={{ display: "grid", gap: 7 }}>
      <label
        htmlFor={htmlFor}
        style={{ fontSize: 12.5, fontWeight: 500, color: "var(--fg)" }}
      >
        {label}
      </label>
      {hint && (
        <p
          style={{
            maxWidth: "60ch",
            fontSize: 12,
            lineHeight: 1.5,
            color: "var(--fg-muted)",
            margin: 0,
          }}
        >
          {hint}
        </p>
      )}
      {children}
    </div>
  );
}

// A read-only fact about the account (email, member since). Rendered as text
// rather than a disabled input, which would imply it is editable somewhere.
export function ReadOnlyRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div style={{ display: "grid", gap: 4 }}>
      <span style={{ fontSize: 12.5, fontWeight: 500, color: "var(--fg)" }}>
        {label}
      </span>
      <span
        style={{
          fontSize: 13,
          color: "var(--fg-muted)",
          fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)",
          fontVariantNumeric: mono ? "tabular-nums" : undefined,
        }}
      >
        {value}
      </span>
    </div>
  );
}

export function SaveButton({
  children = "Save changes",
  saving,
  disabled,
}: {
  children?: ReactNode;
  saving?: boolean;
  disabled?: boolean;
}) {
  const off = saving || disabled;
  return (
    <button
      type="submit"
      className="set-save"
      disabled={off}
      style={{
        justifySelf: "start",
        height: 34,
        padding: "0 16px",
        background: "var(--accent)",
        color: "var(--accent-fg)",
        border: "none",
        borderRadius: "var(--r-2)",
        fontSize: 13,
        fontWeight: 600,
        fontFamily: "var(--font-sans)",
        cursor: off ? "not-allowed" : "pointer",
        opacity: off ? 0.6 : 1,
      }}
    >
      {saving ? "Saving…" : children}
    </button>
  );
}

// Inline result of a save. Errors carry the server's own message, so a failure
// says what actually went wrong rather than a generic "something broke".
export function FormStatus({
  state,
  message,
}: {
  state: "idle" | "saving" | "saved" | "error";
  message?: string;
}) {
  if (state === "idle" || state === "saving" || !message) return null;
  return (
    <p
      role="status"
      className="set-status"
      style={{
        margin: 0,
        fontSize: 12,
        fontFamily: "var(--font-mono)",
        color: state === "error" ? "var(--danger)" : "var(--accent)",
      }}
    >
      {message}
    </p>
  );
}

/**
 * A date rendered in UTC.
 *
 * Pinned deliberately. Without an explicit timeZone the same instant formats a
 * day earlier for every viewer west of the server -- 2026-01-01T00:00:00Z is
 * "January 1, 2026" in UTC and "December 31, 2025" across the Americas. This
 * page shipped that bug once already, so the formatting lives here rather than
 * being retyped per section.
 */
export function formatDateUTC(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? "—"
    : new Intl.DateTimeFormat("en", {
        year: "numeric",
        month: "long",
        day: "numeric",
        timeZone: "UTC",
      }).format(d);
}

/**
 * Money conversions for the settings forms.
 *
 * `microsToUSD` deliberately does not pad to two decimals. These strings seed
 * *editable* inputs, and `toFixed(2)` is lossy: a stored ceiling of 50_500
 * micros ($0.0505) would render "0.05", and saving the untouched form would
 * write back 50_000 -- silently lowering a limit the user never touched.
 * String() round-trips exactly, at the cost of showing "5" rather than "5.00".
 */
export const microsToUSD = (micros: number): string => String(micros / 1e6);

export const usdToMicros = (usd: number): number => Math.round(usd * 1e6);

/**
 * The same conversion as microsToUSD, kept numeric.
 *
 * Callers that feed a number (the credit store's threshold) cannot use the
 * string form, and open-coding `/ 1e6` at those sites is how the two drift
 * apart. Both helpers divide identically, so they cannot disagree.
 */
export const microsToUSDNumber = (micros: number): number => micros / 1e6;

export type SaveState = "idle" | "saving" | "saved" | "error";

/**
 * The idle/saving/saved/error machine every settings form runs.
 *
 * It was retyped in five places — Account twice, Billing, Execution and
 * Connections — each with its own state pair and its own try/catch. The
 * rendering half was already shared (SaveButton, FormStatus); this is the
 * missing half, so a future change (auto-clearing "saved", say) lands once.
 *
 * `run` reports the server's own message on failure rather than a generic
 * string, because those messages carry real distinctions — "current password is
 * incorrect" versus an OAuth-only account with no password to change.
 */
export function useSaveState(): {
  state: SaveState;
  message: string;
  fail: (message: string) => void;
  run: (action: () => Promise<void>, onSaved: string) => Promise<void>;
} {
  const [state, setState] = useState<SaveState>("idle");
  const [message, setMessage] = useState("");

  const fail = useCallback((m: string) => {
    setState("error");
    setMessage(m);
  }, []);

  const run = useCallback(
    async (action: () => Promise<void>, onSaved: string) => {
      setState("saving");
      setMessage("");
      try {
        await action();
        setState("saved");
        setMessage(onSaved);
      } catch (err) {
        setState("error");
        setMessage(err instanceof Error ? err.message : "Could not save.");
      }
    },
    [],
  );

  return { state, message, fail, run };
}
