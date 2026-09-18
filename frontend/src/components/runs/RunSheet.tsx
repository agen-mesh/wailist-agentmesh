"use client";
import { useEffect, useRef } from "react";
import { useScrollLock } from "@/hooks/useScrollLock";
import { useCloseOnBack } from "@/hooks/useCloseOnBack";
import { useNow } from "@/hooks/useNow";
import { ghostBtn } from "@/components/ui/buttons";
import { MarkdownContent } from "@/components/canvas/chat/MarkdownContent";
import {
  isX402Payment,
  settledUsdOf,
} from "@/components/canvas/useRunTranscript";
import type { RunLogRecord } from "@/lib/api";
import type { RunStatus, RunSummary } from "@/lib/types";
import {
  formatDuration,
  formatRunTime,
  formatSpend,
  triggerLabel,
} from "@/lib/runFormat";
import { RunStatusPill } from "./RunStatusPill";
import { useRunDetail } from "./useRunDetail";

// What one run did: its result, its steps and what it paid for.
//
// A bottom sheet on a phone and a centred dialog on a wider screen, built the
// same way as NotificationsSheet: a scoped <style> block for what inline
// styles cannot express, inline styles for the rest.
const SHEET_CSS = `
.run-sheet-scrim {
  position: fixed; inset: 0; z-index: 1000;
  background: rgba(8,7,12,0.72); backdrop-filter: blur(4px);
  display: flex; align-items: flex-end; justify-content: center;
  animation: run-sheet-scrim-in 0.18s var(--ease);
}
.run-sheet-panel {
  position: relative;
  width: 100%; max-width: min(560px, 100vw);
  max-height: 88dvh; overflow-y: auto; overscroll-behavior: contain;
  padding: 20px calc(20px + var(--safe-right, 0px)) calc(20px + var(--safe-bottom, 0px)) calc(20px + var(--safe-left, 0px));
  border: 1px solid var(--border-strong);
  border-radius: var(--r-4) var(--r-4) 0 0;
  background: var(--bg-elev-1);
  color: var(--fg);
  box-shadow: 0 -12px 40px rgba(0,0,0,0.55);
  animation: run-sheet-panel-in 0.22s var(--ease);
}
.run-sheet-panel:focus { outline: none; }
.run-sheet-action {
  transition: color 0.12s var(--ease), border-color 0.12s var(--ease),
    transform 0.12s var(--ease);
}
.run-sheet-action:active { transform: scale(0.97); }
.run-sheet-action:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
@keyframes run-sheet-scrim-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes run-sheet-panel-in {
  from { opacity: 0; transform: translateY(12px) scale(0.985); }
  to { opacity: 1; transform: none; }
}
@media (min-width: 640px) {
  .run-sheet-scrim { align-items: center; padding: 24px; }
  .run-sheet-panel { border-radius: var(--r-4); box-shadow: 0 24px 64px rgba(0,0,0,0.55); }
}
@media (prefers-reduced-motion: reduce) {
  .run-sheet-scrim, .run-sheet-panel { animation: none; }
  .run-sheet-action { transition: none; }
  .run-sheet-action:active { transform: none; }
}
`;

const RUN_STATUSES: readonly string[] = [
  "running",
  "success",
  "failed",
  "stopped",
] satisfies RunStatus[];

function isRunStatus(s: string | undefined): s is RunStatus {
  return s !== undefined && RUN_STATUSES.includes(s);
}

function stepName(log: RunLogRecord): string {
  const output = log.output as { nodeName?: unknown } | null | undefined;
  if (output && typeof output.nodeName === "string" && output.nodeName) {
    return output.nodeName;
  }
  return log.nodeType.charAt(0).toUpperCase() + log.nodeType.slice(1);
}

// The answer to show: the last successful step that produced a message.
function resultText(steps: RunLogRecord[]): string | null {
  for (let i = steps.length - 1; i >= 0; i--) {
    const output = steps[i].output as { message?: unknown } | null | undefined;
    if (
      steps[i].status === "success" &&
      output &&
      typeof output.message === "string" &&
      output.message.trim()
    ) {
      return output.message;
    }
  }
  return null;
}

const STEP_STATUS: Record<RunLogRecord["status"], string> = {
  pending: "Waiting",
  running: "Running",
  success: "Done",
  failed: "Failed",
  // Finished, but only partly: the run carried on instead of failing.
  degraded: "Degraded",
};

export function RunSheet({
  run,
  onClose,
  returnFocusTo,
}: {
  // The row that was tapped. Shown straight away, then refined by the detail.
  run: RunSummary;
  onClose: () => void;
  // Where focus goes on close: the row that opened the sheet.
  returnFocusTo?: React.RefObject<HTMLElement | null>;
}) {
  const detail = useRunDetail(run.id);
  const panelRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<Element | null>(null);

  useScrollLock(true);

  useEffect(() => {
    openerRef.current = document.activeElement;
    // On the next task, as NotificationsSheet does: focus moved in this same
    // task can be undone by the tap that opened the sheet.
    const t = window.setTimeout(() => panelRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, []);

  // Every way out, the system Back gesture included, hands focus back to the
  // row that opened the sheet.
  const close = useCloseOnBack(() => {
    onClose();
    const opener = openerRef.current;
    const fallback =
      opener instanceof HTMLElement && opener.isConnected ? opener : null;
    (returnFocusTo?.current ?? fallback)?.focus();
  });
  const closeRef = useRef(close);
  useEffect(() => {
    closeRef.current = close;
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const detailStatus = detail.run?.status;
  const status = isRunStatus(detailStatus) ? detailStatus : run.status;
  // Both ends from the same record, so the duration never pairs the row's
  // start with the detail's finish.
  const startedAt = detail.run?.startedAt ?? run.startedAt;
  const finishedAt = detail.run ? detail.run.finishedAt : run.finishedAt;
  const running = status === "running";
  // Prefer the polled detail's spend — it grows while the run is still
  // going. The static `run` prop is only what was known when the sheet
  // opened, the same fallback shape already used for startedAt above.
  const spendUsdMicros = detail.run?.spendUsdMicros ?? run.spendUsdMicros;
  const now = useNow(running);
  const steps = [...detail.logs].sort((a, b) => a.stepIndex - b.stepIndex);
  const result = resultText(steps);
  const payments = steps
    .filter((l) => isX402Payment(l.output))
    .map((l) => ({
      id: l.id,
      name: stepName(l),
      usd: settledUsdOf(l.output),
    }));
  const titleId = `run-sheet-title-${run.id}`;

  return (
    <>
      <style>{SHEET_CSS}</style>
      <div
        className="run-sheet-scrim"
        onClick={(e) => {
          if (e.target === e.currentTarget) close();
        }}
      >
        <div
          className="run-sheet-panel"
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabIndex={-1}
          ref={panelRef}
        >
          <h2 id={titleId} style={heading}>
            {run.workflowName}
          </h2>
          <div style={metaRow}>
            <RunStatusPill status={status} />
            <span>{triggerLabel(run.triggeredBy)}</span>
            <span aria-hidden>·</span>
            <span>{formatRunTime(run.startedAt)}</span>
          </div>

          <dl style={facts}>
            <div>
              <dt style={factLabel}>{running ? "Running for" : "Took"}</dt>
              <dd style={factValue}>
                {formatDuration(startedAt, finishedAt, now)}
              </dd>
            </div>
            <div>
              <dt style={factLabel}>{running ? "Spent so far" : "Spent"}</dt>
              <dd style={factValue}>{formatSpend(spendUsdMicros)}</dd>
            </div>
          </dl>

          {detail.loading && <p style={bodyText}>Loading this run…</p>}
          {detail.error && (
            <p role="alert" style={{ ...bodyText, color: "var(--danger)" }}>
              {detail.error}
            </p>
          )}

          {result && (
            <section style={section} aria-label="Result">
              <h3 style={sectionLabel}>Result</h3>
              <div style={resultBox}>
                <MarkdownContent text={result} />
              </div>
            </section>
          )}

          {steps.length > 0 && (
            <section style={section} aria-label="Steps">
              <h3 style={sectionLabel}>Steps</h3>
              <ol style={list}>
                {steps.map((s) => (
                  <li key={s.id} style={listRow}>
                    <span style={rowName}>{stepName(s)}</span>
                    <span
                      style={{
                        ...rowAside,
                        color:
                          s.status === "failed"
                            ? "var(--danger)"
                            : "var(--fg-muted)",
                      }}
                    >
                      {STEP_STATUS[s.status]}
                      {typeof s.durationMs === "number" &&
                        ` · ${(s.durationMs / 1000).toFixed(1)}s`}
                    </span>
                  </li>
                ))}
              </ol>
            </section>
          )}

          {payments.length > 0 && (
            <section style={section} aria-label="Payments">
              <h3 style={sectionLabel}>Payments</h3>
              <ul style={list}>
                {payments.map((p) => (
                  <li key={p.id} style={listRow}>
                    <span style={rowName}>{p.name}</span>
                    <span style={{ ...rowAside, color: "var(--fg)" }}>
                      {p.usd === null
                        ? "Paid"
                        : formatSpend(Math.round(p.usd * 1e6))}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {detail.deadLetters.length > 0 && (
            <section style={section} aria-label="Problems">
              <h3 style={sectionLabel}>Problems</h3>
              <ul style={list}>
                {detail.deadLetters.map((d) => (
                  <li
                    key={d.id}
                    style={{
                      ...listRow,
                      display: "block",
                      color: "var(--danger)",
                    }}
                  >
                    {d.error}
                  </li>
                ))}
              </ul>
            </section>
          )}

          <div style={actions}>
            <button
              type="button"
              className="run-sheet-action"
              style={{ ...ghostBtn, minHeight: 44 }}
              onClick={close}
            >
              Close
            </button>
          </div>
        </div>
      </div>
    </>
  );
}

const heading: React.CSSProperties = {
  margin: "0 0 8px",
  font: "600 17px/1.3 var(--font-sans)",
  color: "var(--fg)",
  overflowWrap: "anywhere",
};

const metaRow: React.CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  alignItems: "center",
  gap: 8,
  font: "400 12px/1.4 var(--font-sans)",
  color: "var(--fg-muted)",
};

const facts: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
  gap: 12,
  margin: "16px 0 4px",
};

const factLabel: React.CSSProperties = {
  font: "500 11px/1 var(--font-mono)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
};

const factValue: React.CSSProperties = {
  margin: "6px 0 0",
  font: "500 15px/1.2 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  color: "var(--fg)",
};

const bodyText: React.CSSProperties = {
  margin: "12px 0 0",
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg-muted)",
};

const section: React.CSSProperties = { marginTop: 20 };

const sectionLabel: React.CSSProperties = {
  margin: "0 0 8px",
  font: "500 11px/1 var(--font-mono)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
};

const resultBox: React.CSSProperties = {
  padding: "10px 12px",
  borderRadius: "var(--r-2)",
  border: "1px solid var(--border)",
  background: "var(--bg)",
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg)",
  overflowWrap: "anywhere",
};

const list: React.CSSProperties = {
  listStyle: "none",
  margin: 0,
  padding: 0,
  borderTop: "1px solid var(--border)",
};

const listRow: React.CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  alignItems: "baseline",
  gap: 12,
  padding: "10px 0",
  borderBottom: "1px solid var(--border)",
  font: "400 13px/1.4 var(--font-sans)",
  overflowWrap: "anywhere",
};

const rowName: React.CSSProperties = { minWidth: 0, color: "var(--fg)" };

const rowAside: React.CSSProperties = {
  flexShrink: 0,
  font: "400 12px/1.4 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
};

const actions: React.CSSProperties = {
  display: "flex",
  justifyContent: "flex-end",
  marginTop: 20,
};
