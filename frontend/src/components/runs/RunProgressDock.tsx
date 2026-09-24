"use client";
import { useEffect, useRef, useState } from "react";
import { runProgress, type ProgressLog, type RunStep } from "@/lib/runProgress";
import { tapFeedback } from "@/native/haptics";

// What a run is doing, while it does it.
//
// Tapping Run used to change a button and add a row: everything else happened
// somewhere the reader could not see. This docks a line at the bottom of the
// workflow screen with one milestone per node, so a run that takes twenty
// seconds looks like twenty seconds of work rather than a frozen screen.
//
// Docked rather than a sheet on purpose: a sheet would cover the screen the
// run belongs to, and the point is to keep watching it.

const EXPANDED_KEY = "agentmesh.runprogress.expanded";
/** How long a finished run stays on screen before it sees itself out. */
const SUCCESS_LINGER_MS = 4000;

const DOCK_CSS = `
.run-dock {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 50;
  background: var(--bg-elev-2);
  border-top: 1px solid var(--border);
  box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.45);
  /* Fixed to the viewport, so no ancestor's padding reaches it: in landscape
     edge-to-edge a cutout or the system navigation bar would sit on top of
     the headline and the buttons. Both horizontal insets are its own. */
  padding: 10px calc(16px + var(--safe-right, 0px))
    calc(10px + var(--safe-bottom, 0px)) calc(16px + var(--safe-left, 0px));
  /* Expanded, a workflow with enough nodes made the dock taller than a
     compact landscape screen and pushed its own collapse control off the
     top. The bar, the line and the actions always fit; the step list takes
     what is left and scrolls. */
  display: flex;
  flex-direction: column;
  max-height: 70vh;
  max-height: 70svh;
}
.run-dock__bar, .run-dock__line, .run-dock__actions { flex: none; }
.run-dock__bar {
  position: relative;
  height: 4px;
  border-radius: 999px;
  background: var(--bg-elev-3);
  overflow: hidden;
  margin-bottom: 8px;
}
.run-dock__fill {
  position: absolute;
  inset: 0 auto 0 0;
  border-radius: 999px;
  background: var(--accent);
  transition: width 0.35s var(--ease);
}
.run-dock[data-state="failed"] .run-dock__fill { background: var(--danger); }
.run-dock[data-state="success"] .run-dock__fill { background: var(--ok, #3ecf8e); }
.run-dock[data-state="stopped"] .run-dock__fill { background: var(--fg-dim); }
.run-dock__line {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  /* The only way to expand or collapse the dock. At one line tall it was
     under both the 24px WCAG target and the 44px this screen uses
     everywhere else. */
  min-height: 44px;
  padding: 0;
  color: var(--fg);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.run-dock__line:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: 4px;
}
.run-dock__count {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-muted);
  flex: none;
}
.run-dock__now {
  font-size: 12.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.run-dock__chevron { flex: none; color: var(--fg-dim); font-size: 11px; }
.run-dock__steps {
  list-style: none;
  /* Scrolling the steps costs the other axis too: a box with overflow-y set
     computes overflow-x to auto rather than leaving it visible, and the
     working node's dot pulses to 1.5x -- 2px past the content edge on each
     side, which sliced it flat against the list's left edge. The padding
     gives the pulse somewhere to go and the negative margin cancels it, so
     nothing moves. */
  margin: 10px -4px 0;
  padding: 0 4px;
  /* min-height:0 lets a flex child actually shrink and scroll. */
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.run-dock__step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
  font-size: 12.5px;
  color: var(--fg-muted);
}
.run-dock__step[data-state="done"] { color: var(--fg); }
.run-dock__step[data-state="running"] { color: var(--accent); }
.run-dock__step[data-state="failed"] { color: var(--danger); }
.run-dock__step[data-state="skipped"] { color: var(--fg-dim); }
.run-dock__dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: currentColor;
  opacity: 0.35;
  flex: none;
}
.run-dock__step[data-state="done"] .run-dock__dot,
.run-dock__step[data-state="failed"] .run-dock__dot { opacity: 1; }
/* The one thing in motion is the node actually working. */
.run-dock__step[data-state="running"] .run-dock__dot {
  opacity: 1;
  animation: run-dock-pulse 1.2s ease-in-out infinite;
}
.run-dock__took {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-dim);
}
.run-dock__actions { display: flex; gap: 8px; margin-top: 10px; }
.run-dock__actions button:disabled { opacity: 0.55; cursor: default; }
@keyframes run-dock-pulse {
  0%, 100% { transform: scale(1); opacity: 1; }
  50% { transform: scale(1.5); opacity: 0.45; }
}
@media (prefers-reduced-motion: reduce) {
  .run-dock__fill { transition: none; }
  .run-dock__step[data-state="running"] .run-dock__dot { animation: none; }
}
`;

function tookLabel(ms: number | undefined): string {
  if (ms === undefined) return "";
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

function stepMark(state: RunStep["state"]): string {
  if (state === "done") return "✓";
  if (state === "failed") return "✕";
  // A node a finished run never reached. Named rather than ticked: it did
  // not run, and saying it did would be untrue.
  if (state === "skipped") return "not run";
  return "";
}

export function RunProgressDock({
  steps,
  logs,
  runStatus,
  detailError = null,
  busy = false,
  onDetails,
  onRunAgain,
  onDismiss,
}: {
  steps: { id: string; name: string }[];
  logs: ProgressLog[];
  /** The run's own status: running, success, failed or stopped. */
  runStatus: string;
  /**
   * Set when the run's detail could not be read. The dock then says so and
   * offers a way out rather than sitting on a status it cannot confirm.
   */
  detailError?: string | null;
  /** True while a start request is in flight, which disables Run again. */
  busy?: boolean;
  /** Opens the run's sheet, which already shows logs and problems. */
  onDetails: () => void;
  onRunAgain: () => void;
  onDismiss: () => void;
}) {
  const progress = runProgress(steps, logs, runStatus);
  // Remembered per reader, and never load-bearing: a device that refuses
  // storage just gets the collapsed default. Read at mount rather than in an
  // effect -- the dock only appears once a run has started, so there is no
  // server-rendered markup for it to disagree with.
  const [expanded, setExpanded] = useState(() => {
    try {
      return window.localStorage.getItem(EXPANDED_KEY) === "1";
    } catch {
      // Private mode, or storage turned off. The default stands.
      return false;
    }
  });
  // Held in a ref so the timer below need not depend on the caller passing
  // the same function twice. It does not: WorkflowSummary renders these as
  // inline arrows, so each poll makes new ones. Depending on the callback,
  // the effect cleared its own timeout on the next render while the guard
  // stopped it arming another, and a finished run then sat there for good.
  // Found on a device; no test had a parent that re-rendered.
  const dismissRef = useRef(onDismiss);
  useEffect(() => {
    dismissRef.current = onDismiss;
  });
  const toggle = () => {
    setExpanded((was) => {
      const next = !was;
      try {
        window.localStorage.setItem(EXPANDED_KEY, next ? "1" : "0");
      } catch {
        // See above.
      }
      return next;
    });
  };

  // Stop is a terminal status of its own and is read first. Cancelling a node
  // in flight makes runner.go record THAT node as failed before finalising
  // the run as stopped, so `progress.failed` is true for a run the reader
  // stopped on purpose; without this precedence the dock called their own
  // Stop a failure. With no node in flight there is no log at all, and the
  // dock sat on "Starting…" with no way to dismiss it.
  const stopped = runStatus === "stopped";
  const done = !stopped && runStatus === "success";
  // `progress.failed` reads the logs, which can be a stale answer about a
  // run that has since been resumed under the same id. A run something says
  // is going is not a failed one, whatever its last logs said.
  const failed =
    !stopped &&
    (runStatus === "failed" ||
      (progress.failed && runStatus !== "running" && runStatus !== "success"));
  // Nothing more will happen, so the dock offers a way out of all three.
  const over = stopped || done || failed;

  // A run that worked says so and leaves. One that failed or was stopped
  // stays: it is the only thing on screen that can explain what happened.
  useEffect(() => {
    if (!done) return;
    void tapFeedback();
    const timer = window.setTimeout(
      () => dismissRef.current(),
      SUCCESS_LINGER_MS,
    );
    return () => window.clearTimeout(timer);
  }, [done]);

  const state = stopped
    ? "stopped"
    : failed
      ? "failed"
      : done
        ? "success"
        : "running";
  const failedStep = progress.steps.find((s) => s.state === "failed");
  // A detail that cannot be read is reported as that, not as a status. The
  // alternative -- showing the last status it managed to read, or assuming
  // "running" -- states as fact something nothing has confirmed.
  const headline = detailError
    ? "Cannot read this run"
    : stopped
      ? "Stopped"
      : failed
        ? `Failed at ${failedStep?.name ?? "a step"}`
        : done
          ? "Finished"
          : (progress.current?.name ?? "Starting…");

  return (
    <div
      className="run-dock"
      data-state={state}
      role="status"
      aria-live="polite"
      aria-label={`Run progress: ${progress.completed} of ${progress.total} steps. ${headline}`}
    >
      <style>{DOCK_CSS}</style>
      <div className="run-dock__bar">
        <div
          className="run-dock__fill"
          style={{ width: `${progress.percent}%` }}
        />
      </div>
      <button
        type="button"
        className="run-dock__line"
        onClick={toggle}
        aria-expanded={expanded}
      >
        <span className="run-dock__count">
          {progress.completed}/{progress.total}
        </span>
        <span className="run-dock__now">{headline}</span>
        <span className="run-dock__chevron" aria-hidden="true">
          {expanded ? "▾" : "▴"}
        </span>
      </button>

      {expanded && (
        <ol className="run-dock__steps">
          {progress.steps.map((step) => (
            <li
              key={step.id}
              className="run-dock__step"
              data-state={step.state}
            >
              <span className="run-dock__dot" aria-hidden="true" />
              <span className="run-dock__now">{step.name}</span>
              <span className="run-dock__took">
                {stepMark(step.state)} {tookLabel(step.durationMs)}
              </span>
            </li>
          ))}
        </ol>
      )}

      {(over || detailError) && (
        <div className="run-dock__actions">
          <button type="button" onClick={onDetails} style={dockBtn}>
            Details
          </button>
          {(failed || stopped) && (
            <button
              type="button"
              onClick={onRunAgain}
              style={dockBtn}
              disabled={busy}
              aria-busy={busy}
            >
              {busy ? "Starting…" : "Run again"}
            </button>
          )}
          <button
            type="button"
            onClick={onDismiss}
            style={{ ...dockBtn, marginLeft: "auto" }}
          >
            Dismiss
          </button>
        </div>
      )}
    </div>
  );
}

const dockBtn: React.CSSProperties = {
  padding: "7px 12px",
  minHeight: 36,
  fontSize: 12.5,
  borderRadius: 6,
  border: "1px solid var(--border-strong)",
  background: "transparent",
  color: "var(--fg)",
  cursor: "pointer",
};
