"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Pill } from "@/components/ui";
import { Topbar } from "@/components/Topbar";
import { PullToRefresh } from "@/components/PullToRefresh";
import { Skeleton } from "@/components/ui/Skeleton";
import { ghostBtn, primaryBtn } from "@/components/ui/buttons";
import { RunSheet } from "@/components/runs/RunSheet";
import { RunStatusPill } from "@/components/runs/RunStatusPill";
import {
  runs as runsApi,
  workflows as workflowsApi,
  RunsUnavailableError,
} from "@/lib/api";
import type { RunPage, RunStatus, RunSummary, Workflow } from "@/lib/types";
import { groupRunsByDay } from "@/lib/runDays";
import {
  formatDuration,
  formatRunTime,
  formatSpend,
  triggerLabel,
} from "@/lib/runFormat";
import { workflowHref } from "@/lib/routes";

// A workflow as a phone needs it: is it running, what did its runs do and
// cost, and Run or Stop. The graph itself is not shown here; it is edited on a
// desktop, and a read-only canvas on a small screen answered none of these.

const PAGE_SIZE = 20;
// How often the list refreshes while a run is still going.
const POLL_MS = 3_000;

// Newest first, ties by id, as the backend orders them. Rows from `fresh`
// replace rows with the same id in `old`, so a refresh updates a run that was
// already on screen without dropping the older pages below it.
function mergeRuns(fresh: RunSummary[], old: RunSummary[]): RunSummary[] {
  const byId = new Map<string, RunSummary>();
  for (const r of old) byId.set(r.id, r);
  for (const r of fresh) byId.set(r.id, r);
  return [...byId.values()].sort((a, b) =>
    a.startedAt === b.startedAt
      ? b.id.localeCompare(a.id)
      : b.startedAt.localeCompare(a.startedAt),
  );
}

const WORKFLOW_STATUS: Record<
  string,
  { tone: "ok" | "danger" | "default"; label: string }
> = {
  deployed: { tone: "ok", label: "Deployed" },
  error: { tone: "danger", label: "Error" },
  draft: { tone: "default", label: "Draft" },
};

function isChatWorkflow(wf: Workflow): boolean {
  return wf.nodes.some((n) => n.type === "trigger" && n.template === "chat");
}

export function WorkflowSummary({ workflowId }: { workflowId: string }) {
  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [runList, setRunList] = useState<RunSummary[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [runsLoaded, setRunsLoaded] = useState(false);
  const [runsUnavailable, setRunsUnavailable] = useState(false);
  const [runsError, setRunsError] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  // Set once an older page has been appended, so a refresh of the first page
  // does not reset the cursor back to the second page.
  const pagedRef = useRef(false);

  // The run just started from here, shown before the list has caught up.
  const [pending, setPending] = useState<RunSummary | null>(null);
  const [acting, setActing] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const [selected, setSelected] = useState<RunSummary | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  // What a response does to the screen, kept apart from the request so the
  // first load, a pull and the poll all apply it the same way.
  const applyRunPage = useCallback((page: RunPage) => {
    setRunList((prev) => mergeRuns(page.runs, prev));
    if (!pagedRef.current) setNextCursor(page.nextCursor);
    setRunsUnavailable(false);
    setRunsError(null);
    setRunsLoaded(true);
  }, []);
  const applyRunsError = useCallback((e: unknown) => {
    if (e instanceof RunsUnavailableError) setRunsUnavailable(true);
    else setRunsError(e instanceof Error ? e.message : "Could not load runs.");
    setRunsLoaded(true);
  }, []);
  const applyWorkflow = useCallback((wf: Workflow) => {
    setWorkflow(wf);
    setLoadError(null);
  }, []);
  const applyWorkflowError = useCallback((e: unknown) => {
    setLoadError(
      e instanceof Error ? e.message : "Could not load this workflow.",
    );
  }, []);

  const refreshRuns = useCallback(
    () =>
      runsApi
        .listForWorkflow(workflowId, { limit: PAGE_SIZE })
        .then(applyRunPage, applyRunsError),
    [workflowId, applyRunPage, applyRunsError],
  );
  const loadWorkflow = useCallback(
    () => workflowsApi.get(workflowId).then(applyWorkflow, applyWorkflowError),
    [workflowId, applyWorkflow, applyWorkflowError],
  );

  // The first load. State is only set once a response lands, and not at all
  // if the screen has gone by then.
  useEffect(() => {
    let cancelled = false;
    const unlessGone =
      <T,>(apply: (value: T) => void) =>
      (value: T) => {
        if (!cancelled) apply(value);
      };
    workflowsApi
      .get(workflowId)
      .then(unlessGone(applyWorkflow), unlessGone(applyWorkflowError));
    runsApi
      .listForWorkflow(workflowId, { limit: PAGE_SIZE })
      .then(unlessGone(applyRunPage), unlessGone(applyRunsError));
    return () => {
      cancelled = true;
    };
  }, [
    workflowId,
    applyWorkflow,
    applyWorkflowError,
    applyRunPage,
    applyRunsError,
  ]);

  const pendingShown =
    pending && !runList.some((r) => r.id === pending.id) ? pending : null;
  const shown = pendingShown ? [pendingShown, ...runList] : runList;
  const anyRunning = shown.some((r) => r.status === "running");
  const newestRunning = shown[0]?.status === "running";

  // Refresh while something is running and the screen is actually visible. A
  // backgrounded app has nobody to show a status change to.
  useEffect(() => {
    if (!anyRunning) return;
    const pendingId = pendingShown?.id ?? null;
    const timer = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      void refreshRuns();
      // A run the list has not picked up yet is asked about directly, so it
      // still settles when the list is slow to include it.
      if (pendingId) {
        runsApi
          .get(pendingId)
          .then(({ run }) => {
            if (run.status === "running") return;
            setPending((p) =>
              p && p.id === pendingId
                ? {
                    ...p,
                    status: run.status as RunStatus,
                    finishedAt: run.finishedAt,
                  }
                : p,
            );
          })
          .catch(() => {});
      }
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [anyRunning, pendingShown?.id, refreshRuns]);

  const loadMore = async () => {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await runsApi.listForWorkflow(workflowId, {
        cursor: nextCursor,
        limit: PAGE_SIZE,
      });
      pagedRef.current = true;
      setRunList((prev) => mergeRuns(page.runs, prev));
      setNextCursor(page.nextCursor);
    } catch (e) {
      setRunsError(e instanceof Error ? e.message : "Could not load runs.");
    } finally {
      setLoadingMore(false);
    }
  };

  const run = async () => {
    if (!workflow) return;
    setActing(true);
    setActionError(null);
    try {
      const { runId } = await workflowsApi.run(workflowId);
      setPending({
        id: runId,
        workflowId,
        workflowName: workflow.name,
        triggeredBy: "manual",
        status: "running",
        startedAt: new Date().toISOString(),
        spendUsdMicros: 0,
      });
      void refreshRuns();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Could not start a run.");
    } finally {
      setActing(false);
    }
  };

  const stop = async () => {
    setActing(true);
    setActionError(null);
    try {
      await workflowsApi.stop(workflowId);
      await refreshRuns();
    } catch (e) {
      setActionError(
        e instanceof Error ? e.message : "Could not stop the run.",
      );
    } finally {
      setActing(false);
    }
  };

  const refreshAll = useCallback(
    () => Promise.all([loadWorkflow(), refreshRuns()]),
    [loadWorkflow, refreshRuns],
  );

  const chat = workflow ? isChatWorkflow(workflow) : false;
  // A handheld can neither build nor deploy, so whatever is keeping an
  // undeployed workflow from running, the next step is the desktop app.
  const blocked = workflow
    ? chat
      ? "This workflow starts from a chat message — run it from the AgentMesh desktop app"
      : workflow.status !== "deployed"
        ? "Not deployed yet — finish and deploy it in the AgentMesh desktop app"
        : null
    : null;
  const status =
    WORKFLOW_STATUS[workflow?.status ?? "draft"] ?? WORKFLOW_STATUS.draft;
  const hasZone = workflow?.geofenceLat !== undefined;

  return (
    <div
      className="am-viewport"
      style={{
        height: "100dvh",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        background: "var(--bg)",
      }}
    >
      <Topbar />
      <PullToRefresh
        onRefresh={refreshAll}
        style={{ flex: 1, minHeight: 0, background: "var(--bg)" }}
      >
        <main style={page}>
          <Link
            href="/workflows"
            className="wf-summary-action"
            style={{ ...ghostBtn, minHeight: 44, textDecoration: "none" }}
          >
            ← Workflows
          </Link>

          {loadError ? (
            <p role="alert" style={{ ...copy, marginTop: 20 }}>
              {loadError}
            </p>
          ) : !workflow ? (
            <div
              aria-busy="true"
              style={{ marginTop: 20, display: "grid", gap: 10 }}
            >
              <Skeleton width="60%" height={22} />
              <Skeleton width="40%" height={14} />
              <Skeleton width="100%" height={44} radius="var(--r-2)" />
            </div>
          ) : (
            <header style={{ marginTop: 20 }}>
              <div style={titleRow}>
                <h1 style={title}>{workflow.name}</h1>
                <Pill tone={status.tone} dot mono>
                  {status.label}
                </Pill>
              </div>

              <p style={{ ...copy, marginTop: 6 }}>
                {chat
                  ? "Starts from a chat message"
                  : workflow.scheduleCron
                    ? "Runs on a schedule · "
                    : "Runs when started"}
                {!chat && workflow.scheduleCron && (
                  <code style={cron}>{workflow.scheduleCron}</code>
                )}
              </p>

              {workflow.status === "deployed" && (
                <Link
                  href={workflowHref(workflowId, { geofence: true })}
                  className="wf-summary-action"
                  style={zoneLink}
                >
                  {hasZone
                    ? "Location zone set · Change"
                    : "Add a location zone"}
                </Link>
              )}

              <div style={{ marginTop: 16 }}>
                {newestRunning ? (
                  <button
                    type="button"
                    className="wf-summary-action"
                    style={{ ...ghostBtn, ...fullWidth }}
                    onClick={stop}
                    disabled={acting}
                  >
                    {acting ? "Stopping…" : "Stop"}
                  </button>
                ) : (
                  <button
                    type="button"
                    className="wf-summary-action"
                    style={{
                      ...primaryBtn,
                      ...fullWidth,
                      opacity: blocked ? 0.5 : 1,
                    }}
                    onClick={run}
                    disabled={acting || blocked !== null}
                    aria-describedby={
                      blocked ? "wf-summary-blocked" : undefined
                    }
                  >
                    {acting ? "Starting…" : "Run"}
                  </button>
                )}
                {blocked && !newestRunning && (
                  <p
                    id="wf-summary-blocked"
                    style={{ ...copy, marginTop: 8, fontSize: 12 }}
                  >
                    {blocked}
                  </p>
                )}
                {actionError && (
                  <p
                    role="alert"
                    style={{
                      ...copy,
                      marginTop: 8,
                      fontSize: 12,
                      color: "var(--danger)",
                    }}
                  >
                    {actionError}
                  </p>
                )}
              </div>
            </header>
          )}

          <section aria-labelledby="wf-summary-runs" style={{ marginTop: 28 }}>
            <h2 id="wf-summary-runs" style={sectionLabel}>
              Recent runs
            </h2>

            {runsUnavailable ? (
              <p style={copy}>
                Run history is not available on this server yet.
              </p>
            ) : !runsLoaded ? (
              <div aria-busy="true" style={{ display: "grid", gap: 8 }}>
                <Skeleton width="100%" height={56} radius="var(--r-2)" />
                <Skeleton width="100%" height={56} radius="var(--r-2)" />
              </div>
            ) : shown.length === 0 ? (
              <p style={copy}>
                {runsError ?? "No runs yet. Runs show up here as they start."}
              </p>
            ) : (
              <>
                {runsError && (
                  <p
                    role="alert"
                    style={{ ...copy, marginBottom: 8, color: "var(--danger)" }}
                  >
                    {runsError}
                  </p>
                )}
                {groupRunsByDay(shown).map((group) => (
                  <div key={group.key} style={{ marginBottom: 16 }}>
                    <h3 style={dayLabel}>{group.label}</h3>
                    <ul style={list}>
                      {group.runs.map((r) => (
                        <li key={r.id}>
                          <button
                            type="button"
                            className="wf-summary-row"
                            style={row}
                            onClick={(e) => {
                              openerRef.current = e.currentTarget;
                              setSelected(r);
                            }}
                          >
                            <span style={rowMain}>
                              <RunStatusPill status={r.status} />
                              <span style={rowMeta}>
                                {triggerLabel(r.triggeredBy)} ·{" "}
                                {formatRunTime(r.startedAt)}
                              </span>
                            </span>
                            <span style={rowFigures}>
                              <span>{formatSpend(r.spendUsdMicros)}</span>
                              <span style={{ color: "var(--fg-dim)" }}>
                                {formatDuration(r.startedAt, r.finishedAt)}
                              </span>
                            </span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
                {nextCursor && (
                  <button
                    type="button"
                    className="wf-summary-action"
                    style={{ ...ghostBtn, ...fullWidth }}
                    onClick={loadMore}
                    disabled={loadingMore}
                  >
                    {loadingMore ? "Loading…" : "Show older runs"}
                  </button>
                )}
              </>
            )}
          </section>
        </main>
      </PullToRefresh>

      <style>{SUMMARY_CSS}</style>
      {selected && (
        <RunSheet
          run={selected}
          onClose={() => setSelected(null)}
          returnFocusTo={openerRef}
        />
      )}
    </div>
  );
}

// Press feedback and focus rings, which inline styles cannot express.
const SUMMARY_CSS = `
.wf-summary-row, .wf-summary-action {
  transition: background 0.12s var(--ease), transform 0.12s var(--ease);
}
.wf-summary-row:active, .wf-summary-action:active { transform: scale(0.98); }
.wf-summary-row:focus-visible, .wf-summary-action:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  .wf-summary-row, .wf-summary-action { transition: none; }
  .wf-summary-row:active, .wf-summary-action:active { transform: none; }
}
`;

const page: React.CSSProperties = {
  maxWidth: 560,
  margin: "0 auto",
  padding: "16px 16px 40px",
};

const titleRow: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 10,
  flexWrap: "wrap",
};

const title: React.CSSProperties = {
  font: "600 20px/1.3 var(--font-sans)",
  color: "var(--fg)",
  margin: 0,
  minWidth: 0,
  overflowWrap: "anywhere",
};

const copy: React.CSSProperties = {
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg-muted)",
  maxWidth: "60ch",
  margin: 0,
};

const cron: React.CSSProperties = {
  font: "500 12px/1 var(--font-mono)",
  color: "var(--fg)",
};

const zoneLink: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  minHeight: 44,
  font: "500 13px/1 var(--font-sans)",
  color: "var(--accent)",
  textDecoration: "none",
};

const fullWidth: React.CSSProperties = {
  width: "100%",
  minHeight: 44,
  justifyContent: "center",
};

const sectionLabel: React.CSSProperties = {
  margin: "0 0 12px",
  font: "500 11px/1 var(--font-mono)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
};

const dayLabel: React.CSSProperties = {
  margin: "0 0 6px",
  font: "500 12px/1 var(--font-sans)",
  color: "var(--fg-muted)",
};

const list: React.CSSProperties = {
  listStyle: "none",
  margin: 0,
  padding: 0,
  display: "grid",
  gap: 6,
};

const row: React.CSSProperties = {
  width: "100%",
  minHeight: 56,
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: 12,
  padding: "8px 12px",
  borderRadius: "var(--r-2)",
  border: "1px solid var(--border)",
  background: "var(--bg-elev-1)",
  color: "var(--fg)",
  textAlign: "left",
  cursor: "pointer",
};

const rowMain: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
  minWidth: 0,
  flexWrap: "wrap",
};

const rowMeta: React.CSSProperties = {
  font: "400 12px/1.4 var(--font-sans)",
  color: "var(--fg-muted)",
};

const rowFigures: React.CSSProperties = {
  display: "grid",
  justifyItems: "end",
  gap: 2,
  flexShrink: 0,
  font: "500 12px/1.3 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
};
