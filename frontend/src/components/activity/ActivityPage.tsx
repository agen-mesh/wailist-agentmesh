"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { Topbar } from "@/components/Topbar";
import { PullToRefresh } from "@/components/PullToRefresh";
import { Skeleton } from "@/components/ui/Skeleton";
import { ghostBtn } from "@/components/ui/buttons";
import { RunSheet } from "@/components/runs/RunSheet";
import { RunStatusPill } from "@/components/runs/RunStatusPill";
import { UpcomingRuns } from "@/components/runs/UpcomingRuns";
import { useNow } from "@/hooks/useNow";
import { usePolling } from "@/hooks/usePolling";
import { runs as runsApi, RunsUnavailableError } from "@/lib/api";
import type { RunPage, RunSummary } from "@/lib/types";
import { groupRunsByDay } from "@/lib/runDays";
import { mergeRuns } from "@/lib/runMerge";
import {
  formatDuration,
  formatRunTime,
  formatSpend,
  triggerLabel,
} from "@/lib/runFormat";

// Everything the signed-in user's workflows have run, newest first, grouped by
// day. Partner-console workflows are left out by the backend, as they are from
// the workflow list.

const PAGE_SIZE = 20;
// How often the list refreshes while a listed run is still going, and while
// nothing listed is, matching WorkflowSummary's own values.
const POLL_MS = 3_000;
const IDLE_POLL_MS = 10_000;

export function ActivityPage() {
  const [runList, setRunList] = useState<RunSummary[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);

  const [selected, setSelected] = useState<RunSummary | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  // A refresh replaces the list with the newest page and starts paging again
  // from there, so a pull never leaves a gap between old and new rows.
  const applyFirstPage = useCallback((page: RunPage) => {
    setRunList(page.runs);
    setNextCursor(page.nextCursor);
    setUnavailable(false);
    setError(null);
    setLoaded(true);
  }, []);
  const applyError = useCallback((e: unknown) => {
    if (e instanceof RunsUnavailableError) setUnavailable(true);
    else setError(e instanceof Error ? e.message : "Could not load activity.");
    setLoaded(true);
  }, []);

  const refresh = useCallback(
    () => runsApi.recent({ limit: PAGE_SIZE }).then(applyFirstPage, applyError),
    [applyFirstPage, applyError],
  );

  // The first load. State is only set once the response lands, and not at all
  // if the screen has gone by then.
  useEffect(() => {
    let cancelled = false;
    runsApi.recent({ limit: PAGE_SIZE }).then(
      (page) => {
        if (!cancelled) applyFirstPage(page);
      },
      (e: unknown) => {
        if (!cancelled) applyError(e);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [applyFirstPage, applyError]);

  const anyRunning = runList.some((r) => r.status === "running");

  // Keep refreshing while the screen is visible, and at once on coming back
  // to it. Runs start in places this screen never hears about (the website,
  // a schedule, a geofence), so an idle list is polled too, only more slowly
  // than one with a run still going.
  //
  // Merged rather than replaced (mergeRuns, not applyFirstPage): a pull is a
  // deliberate "start over from the top" gesture, but a silent background
  // poll must not truncate pages the user has already loaded with
  // "Show older runs".
  //
  // A server that gained run history while the screen was open has nothing
  // listed yet to merge into, so its first page is taken as a fresh load.
  const poll = useCallback(() => {
    runsApi
      .recent({ limit: PAGE_SIZE })
      .then((page) => {
        if (unavailable) applyFirstPage(page);
        else setRunList((prev) => mergeRuns(page.runs, prev));
      })
      .catch(() => {});
  }, [unavailable, applyFirstPage]);
  usePolling(poll, anyRunning ? POLL_MS : IDLE_POLL_MS);

  const now = useNow(anyRunning);

  const loadMore = async () => {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await runsApi.recent({
        cursor: nextCursor,
        limit: PAGE_SIZE,
      });
      setRunList((prev) => [
        ...prev,
        ...page.runs.filter((r) => !prev.some((p) => p.id === r.id)),
      ]);
      setNextCursor(page.nextCursor);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load activity.");
    } finally {
      setLoadingMore(false);
    }
  };

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
        onRefresh={refresh}
        style={{ flex: 1, minHeight: 0, background: "var(--bg)" }}
      >
        <main style={page}>
          <h1 style={title}>Activity</h1>
          <p style={{ ...copy, marginTop: 4 }}>
            What your workflows ran, and what each run spent.
          </p>

          {/* What will run next, above what already ran. Hidden when nothing
              is scheduled, so an unscheduled account sees only its history. */}
          <div style={{ marginTop: 20 }}>
            <UpcomingRuns limit={5} hideWhenEmpty />
          </div>

          <div style={{ marginTop: 20 }}>
            {unavailable ? (
              <p style={copy}>
                Run history is not available on this server yet.
              </p>
            ) : !loaded ? (
              <div aria-busy="true" style={{ display: "grid", gap: 8 }}>
                <Skeleton width="30%" height={12} />
                <Skeleton width="100%" height={64} radius="var(--r-2)" />
                <Skeleton width="100%" height={64} radius="var(--r-2)" />
                <Skeleton width="100%" height={64} radius="var(--r-2)" />
              </div>
            ) : runList.length === 0 ? (
              <p style={copy}>
                {error ??
                  "No runs yet. When one of your workflows runs, it shows up here."}
              </p>
            ) : (
              <>
                {error && (
                  <p
                    role="alert"
                    style={{ ...copy, marginBottom: 8, color: "var(--danger)" }}
                  >
                    {error}
                  </p>
                )}
                {groupRunsByDay(runList).map((group) => (
                  <section
                    key={group.key}
                    aria-labelledby={`activity-day-${group.key}`}
                    style={{ marginBottom: 20 }}
                  >
                    <h2 id={`activity-day-${group.key}`} style={dayLabel}>
                      {group.label}
                    </h2>
                    <ul style={list}>
                      {group.runs.map((r) => (
                        <li key={r.id}>
                          <button
                            type="button"
                            className="activity-row"
                            style={row}
                            onClick={(e) => {
                              openerRef.current = e.currentTarget;
                              setSelected(r);
                            }}
                          >
                            <span style={rowTop}>
                              <span style={rowName}>{r.workflowName}</span>
                              <span style={rowSpend}>
                                {formatSpend(r.spendUsdMicros)}
                              </span>
                            </span>
                            <span style={rowBottom}>
                              <RunStatusPill status={r.status} />
                              <span style={rowMeta}>
                                {triggerLabel(r.triggeredBy)} ·{" "}
                                {formatRunTime(r.startedAt)} ·{" "}
                                {formatDuration(r.startedAt, r.finishedAt, now)}
                              </span>
                            </span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  </section>
                ))}
                {nextCursor && (
                  <button
                    type="button"
                    className="activity-row"
                    style={{
                      ...ghostBtn,
                      width: "100%",
                      minHeight: 44,
                      justifyContent: "center",
                    }}
                    onClick={loadMore}
                    disabled={loadingMore}
                  >
                    {loadingMore ? "Loading…" : "Show older runs"}
                  </button>
                )}
              </>
            )}
          </div>
        </main>
      </PullToRefresh>

      <style>{ACTIVITY_CSS}</style>
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
const ACTIVITY_CSS = `
.activity-row {
  transition: background 0.12s var(--ease), transform 0.12s var(--ease);
}
.activity-row:active { transform: scale(0.98); }
.activity-row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  .activity-row { transition: none; }
  .activity-row:active { transform: none; }
}
`;

const page: React.CSSProperties = {
  maxWidth: 560,
  margin: "0 auto",
  padding: "20px 16px 40px",
};

const title: React.CSSProperties = {
  font: "600 20px/1.3 var(--font-sans)",
  color: "var(--fg)",
  margin: 0,
};

const copy: React.CSSProperties = {
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg-muted)",
  maxWidth: "60ch",
  margin: 0,
};

const dayLabel: React.CSSProperties = {
  margin: "0 0 8px",
  font: "500 11px/1 var(--font-mono)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
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
  minHeight: 64,
  display: "grid",
  gap: 6,
  padding: "10px 12px",
  borderRadius: "var(--r-2)",
  border: "1px solid var(--border)",
  background: "var(--bg-elev-1)",
  color: "var(--fg)",
  textAlign: "left",
  cursor: "pointer",
};

const rowTop: React.CSSProperties = {
  display: "flex",
  alignItems: "baseline",
  justifyContent: "space-between",
  gap: 12,
  minWidth: 0,
};

const rowName: React.CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
  font: "500 14px/1.3 var(--font-sans)",
};

const rowSpend: React.CSSProperties = {
  flexShrink: 0,
  font: "500 12px/1.3 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
};

const rowBottom: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
  flexWrap: "wrap",
  minWidth: 0,
};

const rowMeta: React.CSSProperties = {
  font: "400 12px/1.4 var(--font-sans)",
  color: "var(--fg-muted)",
  fontVariantNumeric: "tabular-nums",
};
