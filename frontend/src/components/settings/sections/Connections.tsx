"use client";
import { useCallback, useEffect, useState } from "react";
import { oauth2, type OAuthCredentialSummary } from "@/lib/api";
import {
  SettingsSection,
  formatDateUTC,
  panelStyle,
} from "@/components/settings/ui";
import { ghostBtnSm } from "@/components/ui";

// Accounts linked through the OAuth credential store. Connecting happens on the
// canvas, where a node needs the account; this page is where they can be
// reviewed and revoked, which the canvas has no place for.
//
// Scope note, deliberately surfaced in the UI below: connector accounts (Slack,
// GitHub, Notion, ...) are a separate system that keeps its tokens on the
// workflow node itself, so they never appear here. Saying nothing would let
// this list imply those accounts were never connected.

function Row({
  cred,
  onRevoked,
}: {
  cred: OAuthCredentialSummary;
  onRevoked: (id: string) => void;
}) {
  // Two-step rather than window.confirm: revoking is irreversible and breaks
  // any workflow using the account, so it deserves a deliberate second click --
  // but a native dialog is unstyleable and can be suppressed by the browser.
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const revoke = async () => {
    setBusy(true);
    setError("");
    try {
      await oauth2.deleteCredential(cred.id);
      onRevoked(cred.id);
    } catch (err) {
      setBusy(false);
      setConfirming(false);
      setError(
        err instanceof Error
          ? err.message
          : "Could not disconnect this account.",
      );
    }
  };

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 16,
        padding: "12px 0",
        borderTop: "1px solid var(--border-soft)",
      }}
    >
      <div style={{ display: "grid", gap: 3, minWidth: 0 }}>
        <span style={{ fontSize: 13, color: "var(--fg)", fontWeight: 500 }}>
          {cred.accountLabel || "Unnamed account"}
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            color: "var(--fg-dim)",
            letterSpacing: "0.04em",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {cred.provider} · connected {formatDateUTC(cred.createdAt)}
        </span>
        {error && (
          <span style={{ fontSize: 12, color: "var(--danger)" }}>{error}</span>
        )}
      </div>

      {confirming ? (
        <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
          <button
            type="button"
            onClick={revoke}
            disabled={busy}
            style={{
              ...ghostBtnSm,
              color: "var(--danger)",
              borderColor: "var(--danger)",
            }}
          >
            {busy ? "Disconnecting…" : "Confirm"}
          </button>
          <button
            type="button"
            onClick={() => setConfirming(false)}
            disabled={busy}
            style={ghostBtnSm}
          >
            Cancel
          </button>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setConfirming(true)}
          style={{ ...ghostBtnSm, flexShrink: 0 }}
          aria-label={`Disconnect ${cred.accountLabel || cred.provider}`}
        >
          Disconnect
        </button>
      )}
    </div>
  );
}

export function ConnectionsSection() {
  const [creds, setCreds] = useState<OAuthCredentialSummary[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    oauth2
      .listCredentials()
      .then((list) => {
        if (live) setCreds(list);
      })
      .catch(() => {
        // Distinguished from "none connected": showing an empty list when the
        // request failed would tell the user their accounts are gone.
        if (live) {
          setCreds([]);
          setError("Could not load your connected accounts.");
        }
      });
    return () => {
      live = false;
    };
  }, []);

  const onRevoked = useCallback((id: string) => {
    setCreds((prev) => (prev ?? []).filter((c) => c.id !== id));
  }, []);

  return (
    <SettingsSection
      id="connections"
      title="Connected accounts"
      description="Accounts you have linked so your agents can act on your behalf. Disconnecting one stops any workflow that relies on it."
    >
      <div style={panelStyle}>
        {creds === null && (
          <span style={{ fontSize: 13, color: "var(--fg-muted)" }}>
            Loading…
          </span>
        )}

        {error && (
          <span role="alert" style={{ fontSize: 13, color: "var(--danger)" }}>
            {error}
          </span>
        )}

        {creds !== null && !error && creds.length === 0 && (
          <span style={{ fontSize: 13, color: "var(--fg-muted)" }}>
            No accounts connected yet. You connect one from a node on the
            canvas, at the point a workflow needs it.
          </span>
        )}

        {creds?.map((c) => (
          <Row key={c.id} cred={c} onRevoked={onRevoked} />
        ))}
      </div>

      {/* Without this the list quietly implies Slack/GitHub were never
          connected, when in fact they are held on the node itself. */}
      <p
        style={{
          margin: "10px 0 0",
          fontSize: 12,
          color: "var(--fg-dim)",
          maxWidth: "60ch",
        }}
      >
        Connector accounts such as Slack, GitHub and Notion are stored on the
        workflow node that uses them, and are managed there rather than here.
      </p>
    </SettingsSection>
  );
}
