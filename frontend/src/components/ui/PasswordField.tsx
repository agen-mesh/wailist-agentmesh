"use client";
import { useState, type CSSProperties } from "react";

// One password input plus its reveal toggle, shared by the sign-in form and the
// settings change-password form.
//
// Extracted rather than copied. The two screens previously solved this
// separately and drifted: sign-in had the toggle with proper aria state, and
// settings had three bare inputs. Divergent copies of one control are how a fix
// lands on one screen and not the other, so there is now a single
// implementation and both screens import it.
//
// `style` is a prop because the two callers legitimately differ -- sign-in has
// its own input styling and settings uses the one from settings/ui.tsx.
export function PasswordField({
  id,
  value,
  onChange,
  style,
  autoComplete,
  placeholder,
  required,
  describedBy,
}: {
  id?: string;
  value: string;
  onChange: (next: string) => void;
  style: CSSProperties;
  autoComplete?: string;
  placeholder?: string;
  required?: boolean;
  describedBy?: string;
}) {
  const [shown, setShown] = useState(false);

  return (
    <div style={{ position: "relative" }}>
      <input
        id={id}
        // Swapping type is what actually reveals the value; the button only
        // drives this. Kept on one input so the field never loses focus or
        // selection when toggled.
        type={shown ? "text" : "password"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        placeholder={placeholder}
        required={required}
        aria-describedby={describedBy}
        // Leaves room for the toggle so typed text never runs under it.
        style={{ ...style, paddingRight: 64 }}
      />
      <button
        type="button"
        className="pw-reveal"
        onClick={() => setShown((v) => !v)}
        // aria-pressed carries the on/off state; the label says what the
        // control will do next, which is what a screen reader should announce.
        aria-pressed={shown}
        aria-label={shown ? "Hide password" : "Show password"}
        style={{
          position: "absolute",
          top: 0,
          right: 0,
          height: "100%",
          padding: "0 10px",
          background: "transparent",
          border: "none",
          color: "var(--fg-muted)",
          fontSize: 11,
          fontFamily: "var(--font-mono)",
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          cursor: "pointer",
        }}
      >
        {shown ? "Hide" : "Show"}
      </button>
    </div>
  );
}
