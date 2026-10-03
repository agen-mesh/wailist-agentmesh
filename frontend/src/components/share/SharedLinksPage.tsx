"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Topbar } from "@/components/Topbar";
import { shares as sharesApi } from "@/lib/api";
import { workflowHref } from "@/lib/routes";
import type { UserShare } from "@/lib/types";

// Every link this account has out, in one place.
//
// This screen exists because the allowance is counted PER USER while the only
// way to see a link was inside the Share dialog of one workflow. Somebody at
// the ceiling was told "revoke one before creating another" with nowhere that
// would show them what they had -- a dead end you could only escape by
// opening workflows one at a time and guessing.
//
// It is also the honest answer to a question the dialog cannot reach: what
// have I actually published?

const isLive = (s: UserShare) =>
  !s.revokedAt &&
  (!s.expiresAt || new Date(s.expiresAt).getTime() > Date.now());

function lifeOf(s: UserShare): { label: string; dead: boolean } {
  if (s.revokedAt) return { label: "Revoked", dead: true };
  if (!s.expiresAt) return { label: "Never expires", dead: false };
  const ms = new Date(s.expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms)) return { label: "Never expires", dead: false };
  if (ms <= 0) return { label: "Expired", dead: true };
  const days = Math.ceil(ms / 86_400_000);
  if (days === 1) return { label: "Expires tomorrow", dead: false };
  return { label: `Expires in ${days} days`, dead: false };
}

export function SharedLinksPage() {
  const [rows, setRows] = useState<UserShare[] | null>(null);
  const [limit, setLimit] = useState(0);
  const [error, setError] = useState<string | null>(null);
  // One armed row at a time; revoking cannot be undone, so it asks first.
  const [confirming, setConfirming] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await sharesApi.listMine();
        if (cancelled) return;
        setRows(res.shares);
        setLimit(res.limit);
      } catch (e) {
        if (!cancelled) {
          setError(
            e instanceof Error ? e.message : "could not load your share links",
          );
          setRows([]);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const revoke = async (token: string) => {
    if (confirming !== token) {
      setConfirming(token);
      return;
    }
    setConfirming(null);
    setError(null);
    try {
      await sharesApi.revoke(token);
      // Marked in place rather than removed: seeing it struck through is the
      // confirmation that the press did what it said it would.
      const stamp = new Date().toISOString();
      setRows(
        (prev) =>
          prev?.map((s) =>
            s.token === token ? { ...s, revokedAt: stamp } : s,
          ) ?? prev,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not revoke that link");
    }
  };

  const live = rows?.filter(isLive).length ?? 0;

  return (
    <div
      style={{
        height: "100dvh",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        background: "var(--bg)",
      }}
    >
      <Topbar />
      <div style={{ flex: 1, minHeight: 0, overflow: "auto" }}>
        <main style={page}>
          <h1 style={title}>Shared links</h1>
          <p style={subtitle}>
            Anyone holding one of these can open the workflow and import a copy
            of it. Revoking is immediate and cannot be undone.
          </p>

          {rows !== null && rows.length > 0 && (
            <p style={counter}>
              {live} live{limit > 0 ? ` of ${limit}` : ""}
            </p>
          )}

          {error && <p style={errorLine}>{error}</p>}

          {rows === null && <p style={muted}>Loading…</p>}

          {rows !== null && rows.length === 0 && !error && (
            <p style={muted}>
              You haven&apos;t shared anything yet. Open a workflow and press
              Share to hand someone a link.
            </p>
          )}

          {rows !== null && rows.length > 0 && (
            <ul style={list}>
              {rows.map((s) => {
                const life = lifeOf(s);
                return (
                  <li key={s.token} style={row}>
                    <div style={{ minWidth: 0 }}>
                      <Link
                        href={workflowHref(s.workflowId)}
                        style={{
                          ...rowName,
                          opacity: life.dead ? 0.55 : 1,
                          textDecoration: life.dead ? "line-through" : "none",
                        }}
                      >
                        {s.workflowName}
                      </Link>
                      <span style={rowMeta}>
                        …{s.token.slice(-8)} · {s.nodeCount} nodes ·{" "}
                        {s.importCount ?? 0} imports · {life.label}
                      </span>
                    </div>
                    {isLive(s) && (
                      <button
                        type="button"
                        onClick={() => void revoke(s.token)}
                        className="share-revoke-btn"
                      >
                        {confirming === s.token ? "Revoke?" : "Revoke"}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </main>
      </div>
    </div>
  );
}

const page: React.CSSProperties = {
  maxWidth: 680,
  margin: "0 auto",
  padding: "20px 16px 40px",
};

const title: React.CSSProperties = {
  font: "600 20px/1.3 var(--font-sans)",
  color: "var(--fg)",
  margin: 0,
};

const subtitle: React.CSSProperties = {
  margin: "8px 0 0",
  fontSize: 13,
  lineHeight: 1.6,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const counter: React.CSSProperties = {
  margin: "14px 0 0",
  fontFamily: "var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  fontSize: 11.5,
  color: "var(--fg-dim)",
};

const muted: React.CSSProperties = {
  margin: "20px 0 0",
  fontSize: 13,
  lineHeight: 1.6,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const errorLine: React.CSSProperties = {
  margin: "16px 0 0",
  fontSize: 12.5,
  color: "var(--danger)",
  maxWidth: "60ch",
};

const list: React.CSSProperties = {
  listStyle: "none",
  margin: "16px 0 0",
  padding: 0,
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  overflow: "hidden",
};

const row: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: 12,
  padding: "12px 14px",
  borderTop: "1px solid var(--border)",
  background: "var(--bg-elev-1)",
};

const rowName: React.CSSProperties = {
  display: "block",
  fontSize: 13.5,
  fontWeight: 600,
  color: "var(--fg)",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

const rowMeta: React.CSSProperties = {
  display: "block",
  marginTop: 3,
  fontFamily: "var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  fontSize: 11,
  color: "var(--fg-dim)",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};
