"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { Pill } from "@/components/ui";
import {
  runs as runsApi,
  auth as authApi,
  type RunLogRecord,
} from "@/lib/api";
import { recordSettlements } from "@/lib/settlements";
import { TerminalTab } from "./TerminalTab";

interface LogEvent {
  stepIndex: number;
  nodeId: string;
  nodeType: string;
  status: "running" | "success" | "failed" | "stopped";
  output: unknown;
  durationMs: number;
  ts: string;
}

interface X402Payment {
  txId: string;
  amount?: string;
  explorerURL?: string;
  // outboundTxId/outboundExplorerURL are the SECOND real settlement leg --
  // txId above is always the inbound leg (caller -> Wallet 2), this is
  // Wallet 2 -> the actual target, when the target returned one (not
  // every target does).
  outboundTxId?: string;
  outboundExplorerURL?: string;
  nodeName?: string;
  nodeId?: string;
}

function isX402Payment(output: unknown): output is X402Payment {
  return typeof output === "object" && output !== null && "txId" in output;
}

interface LogDrawerProps {
  open: boolean;
  onToggle: () => void;
  runId: string | null;
  running: boolean;
  onRunComplete: () => void;
  // Scopes the cached last-run transcript, so reopening a workflow shows that
  // workflow's own last result rather than whatever ran most recently.
  workflowId?: string;
}

const HEIGHT_KEY = "agentmesh_console_height";
const DEFAULT_HEIGHT = 240;
const MIN_HEIGHT = 120;
// Leave room for the topbar and some canvas: a console dragged to fill the
// window would hide the graph it is reporting on.
const maxHeight = () =>
  typeof window === "undefined" ? 640 : Math.max(MIN_HEIGHT, window.innerHeight - 220);

const RUN_CACHE_PREFIX = "agentmesh_lastrun_";
// localStorage is ~5 MB per origin and a single x402 response can be tens of
// KB. Cap what we cache so one large transcript can't evict everything else
// (or throw on write) — the live view is never truncated, only the cache.
const MAX_CACHED_RUN_BYTES = 512 * 1024;

interface CachedRun {
  runId: string;
  logs: LogEvent[];
  elapsed: number | null;
}

function readCachedRun(workflowId: string | undefined): CachedRun | null {
  if (!workflowId || typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(RUN_CACHE_PREFIX + workflowId);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as CachedRun;
    return Array.isArray(parsed.logs) ? parsed : null;
  } catch {
    return null;
  }
}

function writeCachedRun(workflowId: string | undefined, run: CachedRun): void {
  if (!workflowId || typeof window === "undefined") return;
  try {
    const serialized = JSON.stringify(run);
    if (serialized.length > MAX_CACHED_RUN_BYTES) return;
    window.localStorage.setItem(RUN_CACHE_PREFIX + workflowId, serialized);
  } catch {
    /* quota or unavailable storage: caching is best-effort */
  }
}

const _CONFIGURED = process.env.NEXT_PUBLIC_API_URL ?? "";

// The SSE stream deliberately bypasses the /api rewrite proxy that every
// other API call in this codebase uses (see lib/api.ts's BASE pattern, kept
// for those calls since they're quick request/response and benefit from the
// same-site cookie handling that proxy exists for). Confirmed live
// (2026-08-01) that Next's rewrite (both `next dev`'s built-in proxy,
// unverified but plausibly also true of Vercel's edge rewrite in
// production) does not correctly hold a long-lived text/event-stream
// connection open: a real x402 settlement taking ~30s+ (a completely
// normal duration for a real facilitator+algod round trip) caused the
// proxied stream to silently fail with a bare 500 after ~30s, never
// delivering a single log event, while the exact same run watched directly
// against the backend delivered every event correctly. The frontend has no
// way to tell "the run legitimately finished with nothing to show" apart
// from "my SSE connection just broke" (see the identical es.onerror
// handling below), so this presented as "run complete · 0/0 nodes
// succeeded" for a run that had, in fact, fully succeeded and moved real
// money.
//
// Safe to go directly to the backend here specifically because it already
// sets the auth cookie as SameSite=None; Secure whenever its own BASE_URL
// is https (auth.go's setAuthCookie) -- exactly the condition under which a
// direct cross-origin EventSource with withCredentials:true correctly
// carries it. That's true in real production (Railway's BASE_URL is https)
// and was true in the local setup this bug was diagnosed against (BASE_URL
// pointed at an https cloudflared tunnel).
const SSE_BASE = _CONFIGURED;

export function LogDrawer({
  open,
  onToggle,
  runId,
  running,
  onRunComplete,
  workflowId,
}: LogDrawerProps) {
  // With no live run, start from this workflow's cached last transcript so
  // reopening it still shows what happened, instead of an empty console.
  const cached = runId ? null : readCachedRun(workflowId);
  const [logs, setLogs] = useState<LogEvent[]>(cached?.logs ?? []);
  const [elapsed, setElapsed] = useState<number | null>(
    cached?.elapsed ?? null,
  );
  const [done, setDone] = useState(!!cached);
  const [height, setHeight] = useState(DEFAULT_HEIGHT);
  const [resizing, setResizing] = useState(false);
  const [tab, setTab] = useState<"logs" | "terminal">("logs");
  const esRef = useRef<EventSource | null>(null);
  const startRef = useRef<number | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // Restore the last chosen console height. Read in an effect rather than in
  // useState's initializer so the server render and first client render agree.
  // Applied on the next frame rather than synchronously: the server renders
  // the default height, so setting the stored one during the effect body
  // would both trip the cascading-render rule and mismatch hydration.
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      try {
        const saved = Number(window.localStorage.getItem(HEIGHT_KEY));
        if (saved >= MIN_HEIGHT) setHeight(Math.min(saved, maxHeight()));
      } catch {
        /* unavailable storage: keep the default */
      }
    });
    return () => cancelAnimationFrame(id);
  }, []);

  // Drag the top edge to resize. Listeners live on the window, not the handle,
  // so a fast drag that outruns the pointer doesn't drop the gesture.
  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    setResizing(true);
    const startY = e.clientY;
    const startHeight = height;
    const onMove = (ev: MouseEvent) => {
      // Dragging up grows the console, hence the inverted delta.
      const next = Math.min(
        maxHeight(),
        Math.max(MIN_HEIGHT, startHeight + (startY - ev.clientY)),
      );
      setHeight(next);
    };
    const onUp = () => {
      setResizing(false);
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      setHeight((h) => {
        try {
          window.localStorage.setItem(HEIGHT_KEY, String(h));
        } catch {
          /* best-effort */
        }
        return h;
      });
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  // Connect SSE for the run. No state resets needed: the parent renders this
  // drawer with key={runId}, so a new run remounts it with fresh initial state.
  useEffect(() => {
    if (!runId) return;
    startRef.current = Date.now();

    // Start elapsed timer
    timerRef.current = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startRef.current!) / 100) / 10);
    }, 100);

    const url = SSE_BASE ? `${SSE_BASE}/runs/${runId}/stream` : null;

    if (!url) return;

    // withCredentials sends the HttpOnly auth cookie automatically.
    const es = new EventSource(url, { withCredentials: true });
    esRef.current = es;

    es.addEventListener("log", (e) => {
      try {
        const ev: LogEvent = JSON.parse((e as MessageEvent).data);
        setLogs((prev) => {
          // Replace running entry for same nodeId, or append
          const idx = prev.findIndex(
            (l) => l.nodeId === ev.nodeId && l.status === "running",
          );
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = ev;
            return next;
          }
          return [...prev, ev];
        });
      } catch {
        /* ignore parse errors */
      }
    });

    // The live stream only delivers events to a client subscribed at the
    // exact moment they're published (broker.go's Publish is a non-blocking,
    // unbuffered-per-subscriber send with no replay) -- a run that finishes
    // a step before this component's SSE connection is fully established
    // drops that step's event silently. Reconciling against the DB-backed
    // /runs/:id record once the stream ends closes that gap: this is what
    // actually fixes "run complete · 0/0 nodes succeeded" for a run that, in
    // truth, ran and failed (or succeeded) with a real, visible reason.
    // The DB is the only reliable source here: the live stream can deliver
    // nothing at all (confirmed 2026-08-02 — a 37s connection that carried a
    // single keepalive and zero events), and even when it works it can miss
    // steps. One shot at stream-close was not enough: a node's row is still
    // `running` until its work finishes and UpdateRunLog lands, so a single
    // fetch fired the moment the stream died filtered that row out and showed
    // an empty console for a run that had really completed and paid. Poll
    // until the run itself reports a terminal status.
    let cancelled = false;
    const TERMINAL = new Set(["success", "failed", "stopped"]);

    const mergeDBLogs = (dbLogs: RunLogRecord[]) => {
      setLogs((prev) => {
        // Merge, never replace: a stream-delivered entry carries the node's
        // full output, so it always wins over the stored row.
        const seen = new Set(prev.map((l) => l.nodeId));
        const missing = dbLogs
          .filter(
            (l) =>
              !seen.has(l.nodeId) &&
              (l.status === "success" || l.status === "failed"),
          )
          .map((l) => ({
            stepIndex: l.stepIndex,
            nodeId: l.nodeId,
            nodeType: l.nodeType,
            status: l.status as "success" | "failed",
            output: l.output,
            durationMs: l.durationMs ?? 0,
            ts: l.ts,
          }));
        if (missing.length === 0) return prev;
        return [...prev, ...missing].sort((a, b) => a.stepIndex - b.stepIndex);
      });
    };

    const reconcile = async () => {
      if (!runId) return;
      // Budget generously. A single agent step routinely runs ~70s (LLM
      // round trips plus a real x402 settlement per tool call, confirmed
      // 2026-08-02), and an agent may take several tool iterations — while
      // the SSE stream dies around 35s, so polling starts long before the
      // run ends. A budget that expires first shows an empty console for a
      // run that completed and charged the user, which is exactly the
      // failure this polling exists to prevent.
      for (let attempt = 0; attempt < 150 && !cancelled; attempt++) {
        try {
          const { run, logs: dbLogs } = await runsApi.get(runId);
          if (cancelled) return;
          if (dbLogs.length > 0) mergeDBLogs(dbLogs);
          if (TERMINAL.has(run.status)) {
            // The run itself is finished — this, not a dropped stream, is
            // what "complete" means.
            clearInterval(timerRef.current!);
            setDone(true);
            onRunComplete();
            return;
          }
        } catch {
          // Transient failure: keep polling rather than giving up on a run
          // whose result is already recorded server-side.
        }
        await new Promise((r) => setTimeout(r, 2000));
      }
    };

    es.addEventListener("done", () => {
      clearInterval(timerRef.current!);
      setDone(true);
      onRunComplete();
      es.close();
      void reconcile();
    });

    // A dropped stream says nothing about the run. EventSource fires onerror
    // on any transient hiccup (including its own reconnect attempts), and
    // treating that as "finished" reported "run complete · 1/1 nodes
    // succeeded" three seconds into a run whose agent still had ~60s of work
    // left — then closed the stream, guaranteeing the real result never
    // arrived live. Hand off to polling instead and let the run's own status
    // decide when it is over.
    es.onerror = () => {
      es.close();
      void reconcile();
    };

    return () => {
      cancelled = true;
      clearInterval(timerRef.current!);
      es.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runId]);

  // Close SSE when stopped externally
  useEffect(() => {
    if (!running && esRef.current) {
      clearInterval(timerRef.current!);
      esRef.current.close();
      esRef.current = null;
    }
  }, [running]);

  // Persist any settlements this run produced, scoped to the signed-in user,
  // so the usage page can show them (see lib/settlements.ts for why this is
  // client-side for now).
  useEffect(() => {
    if (!done || logs.length === 0) return;
    const rows = logs
      .filter((l) => isX402Payment(l.output))
      .map((l) => {
        const p = l.output as X402Payment & { settledUsdMicros?: number };
        return {
          ts: l.ts,
          endpoint: p.nodeName ?? "x402 endpoint",
          amountAlgo: (p.settledUsdMicros ?? 0) / 1e6,
          txId: p.txId,
          explorerURL: p.explorerURL ?? "",
          workflowId: workflowId ?? "",
        };
      });
    if (rows.length === 0) return;
    let stale = false;
    authApi
      .me()
      .then((u) => {
        if (!stale) recordSettlements(u.id, rows);
      })
      .catch(() => {
        /* not signed in / offline: nothing to attribute the rows to */
      });
    return () => {
      stale = true;
    };
  }, [done, logs, workflowId]);

  // Cache the finished transcript for this workflow. Runs after `done` so it
  // captures the reconciled set (including any steps the live stream missed),
  // not a partial mid-run snapshot.
  useEffect(() => {
    if (!done || !runId || logs.length === 0) return;
    writeCachedRun(workflowId, { runId, logs, elapsed });
  }, [done, runId, logs, elapsed, workflowId]);

  // Auto-scroll to bottom on new logs
  useEffect(() => {
    if (open) bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs, open]);

  const statusColor = (s: LogEvent["status"]) => {
    if (s === "success") return "var(--accent)";
    if (s === "failed") return "#F87171";
    if (s === "stopped") return "#FB923C";
    return "var(--fg-dim)";
  };

  const statusLabel = (s: LogEvent["status"]) => {
    if (s === "success") return "OK";
    if (s === "failed") return "ERR";
    if (s === "stopped") return "STP";
    return "RUN";
  };

  // Detect the lease a Tendril rent step in this run opened, so the console
  // can offer a Terminal tab into it. Takes the most recent one — a run
  // renting two machines would need explicit lease-id wiring between nodes,
  // out of scope here (see the plan's "Multiple concurrent leases per run").
  const leaseId = useMemo(() => {
    for (let i = logs.length - 1; i >= 0; i--) {
      const output = logs[i].output;
      if (
        logs[i].nodeType === "tendril" &&
        typeof output === "object" &&
        output !== null &&
        "agentMeshLeaseId" in output
      ) {
        const id = (output as { agentMeshLeaseId?: unknown }).agentMeshLeaseId;
        if (typeof id === "string" && id) return id;
      }
    }
    return null;
  }, [logs]);

  // A run that stops offering a lease (a fresh run with no Tendril step)
  // must not leave the console stuck on a dead Terminal tab — derived at
  // render time rather than synced back via an effect.
  const effectiveTab = leaseId ? tab : "logs";

  const elapsedStr = (elapsed ?? 0).toFixed(1);
  const headerPill = runId
    ? done
      ? `run · ${runId.slice(0, 8)} · ${elapsedStr}s`
      : running
        ? `running · ${elapsedStr}s`
        : `run · ${runId.slice(0, 8)}`
    : "console";

  const pillTone = done ? "ok" : running ? "warm" : "default";

  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 0,
        background: "var(--bg-elev-1)",
        borderTop: "1px solid var(--border)",
        height: open ? height : 32,
        // No animation mid-drag: the tween lags the pointer and feels broken.
        transition: resizing ? "none" : "height .2s cubic-bezier(.2,.8,.2,1)",
        display: "flex",
        flexDirection: "column",
        zIndex: 5,
      }}
    >
      {/* Resize grip. Sits above the header and slightly outside the drawer
          so it stays grabbable without stealing clicks from the header's
          expand/collapse toggle. */}
      {open && (
        <div
          onMouseDown={startResize}
          title="Drag to resize console"
          style={{
            position: "absolute",
            top: -3,
            left: 0,
            right: 0,
            height: 7,
            cursor: "ns-resize",
            zIndex: 6,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              width: 40,
              height: 3,
              borderRadius: 2,
              background: resizing ? "var(--accent)" : "var(--border-strong)",
            }}
          />
        </div>
      )}

      {/* Header bar */}
      <div
        onClick={onToggle}
        style={{
          height: 32,
          padding: "0 14px",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          cursor: "pointer",
          borderBottom: open ? "1px solid var(--border)" : "none",
          flexShrink: 0,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              color: "var(--fg-muted)",
              textTransform: "uppercase",
              letterSpacing: "0.08em",
            }}
          >
            console
          </span>
          <Pill mono tone={pillTone} dot={running}>
            {headerPill}
          </Pill>
          {logs.length > 0 && (
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                color: "var(--fg-dim)",
              }}
            >
              {logs.length} step{logs.length !== 1 ? "s" : ""}
            </span>
          )}
          {leaseId && open && (
            <div
              onClick={(e) => e.stopPropagation()}
              style={{ display: "flex", gap: 4 }}
            >
              {(["logs", "terminal"] as const).map((tb) => (
                <button
                  key={tb}
                  onClick={() => setTab(tb)}
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: 9.5,
                    textTransform: "uppercase",
                    letterSpacing: "0.06em",
                    padding: "2px 8px",
                    borderRadius: 999,
                    border: `1px solid ${tab === tb ? "var(--accent)" : "var(--border)"}`,
                    color: tab === tb ? "var(--accent)" : "var(--fg-dim)",
                    background: "transparent",
                    cursor: "pointer",
                  }}
                >
                  {tb === "logs" ? "Logs" : "Terminal"}
                </button>
              ))}
            </div>
          )}
        </div>
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 5,
            fontFamily: "var(--font-mono)",
            fontSize: 13,
            color: "var(--fg-muted)",
          }}
        >
          <span style={{ fontSize: 15, lineHeight: 1 }}>
            {open ? "▾" : "▴"}
          </span>
          {open ? "collapse" : "expand"}
        </span>
      </div>

      {/* Terminal — mounted only while selected; unmounting on tab switch
          away is intentional (it owns a live WebSocket + SSH session, unlike
          the log list which is cheap to keep mounted underneath). */}
      {open && effectiveTab === "terminal" && leaseId && (
        <div style={{ flex: 1, minHeight: 0 }}>
          <TerminalTab leaseId={leaseId} onClose={() => setTab("logs")} />
        </div>
      )}

      {/* Log lines. Stays mounted (hidden via CSS) when the Terminal tab is
          active, so switching back does not lose the transcript. */}
      {open && (
        <div
          style={{
            display: effectiveTab === "terminal" ? "none" : "block",
            flex: 1,
            overflow: "auto",
            padding: "6px 14px 10px",
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            lineHeight: 1.7,
          }}
        >
          {logs.length === 0 && !running && !runId && (
            <div style={{ color: "var(--fg-dim)", paddingTop: 8 }}>
              run a workflow to see execution logs here.
            </div>
          )}
          {logs.length === 0 && running && (
            <div style={{ color: "var(--fg-dim)", paddingTop: 8 }}>
              waiting for first node…
            </div>
          )}
          {logs.map((l, i) => (
            <div
              key={i}
              style={{
                display: "grid",
                gridTemplateColumns: "52px 34px 110px 1fr",
                gap: 10,
                alignItems: "baseline",
                borderBottom: "1px solid var(--border-soft)",
                padding: "3px 0",
              }}
            >
              <span style={{ color: "var(--fg-dim)", fontSize: 9.5 }}>
                {new Date(l.ts).toLocaleTimeString("en", {
                  hour12: false,
                  hour: "2-digit",
                  minute: "2-digit",
                  second: "2-digit",
                })}
              </span>
              <span
                style={{
                  color: statusColor(l.status),
                  fontWeight: 600,
                  fontSize: 9.5,
                }}
              >
                {statusLabel(l.status)}
              </span>
              <span
                style={{
                  color: "var(--fg-muted)",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                <span style={{ color: nodeTypeColor(l.nodeType) }}>
                  {l.nodeType}
                </span>
                {l.durationMs > 0 && (
                  <span style={{ color: "var(--fg-dim)" }}>
                    {" "}
                    · {l.durationMs}ms
                  </span>
                )}
              </span>
              <OutputCell output={l.output} />
            </div>
          ))}
          {done &&
            (() => {
              const succeeded = logs.filter(
                (l) => l.status === "success",
              ).length;
              const hasFailure = logs.some((l) => l.status === "failed");
              return (
                <div
                  style={{
                    color: hasFailure ? "#F87171" : "var(--accent)",
                    paddingTop: 6,
                    fontSize: 10,
                  }}
                >
                  {hasFailure ? "✕ run failed" : "✓ run complete"} ·{" "}
                  {(elapsed ?? 0).toFixed(1)}s · {succeeded}/{logs.length}{" "}
                  nodes succeeded
                </div>
              );
            })()}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}

function formatSize(chars: number): string {
  return chars < 1024 ? `${chars} B` : `${(chars / 1024).toFixed(1)} KB`;
}

// OutputCell renders a step's result: the settlement receipt (amount + both
// on-chain tx ids) when the step paid for something, AND the payload the
// endpoint actually returned. Previously these were either/or — a paid
// tool402 step matched the receipt branch and its response body was never
// shown at all, so the whole point of the call (the data) was invisible.
// Large payloads collapse behind a toggle rather than flooding the console.
function OutputCell({ output }: { output: unknown }) {
  const [expanded, setExpanded] = useState(false);

  const payment = isX402Payment(output) ? output : null;
  // A paid step's own response lives under `response`; an unpaid step's
  // output IS the payload.
  const payload = payment
    ? (output as { response?: unknown }).response
    : output;

  let text = "";
  if (payload !== null && payload !== undefined) {
    text =
      typeof payload === "string" ? payload : JSON.stringify(payload, null, 2);
  }
  const collapsible = text.length > 200;

  return (
    <div style={{ minWidth: 0 }}>
      {payment && (
        <span
          style={{
            display: "flex",
            alignItems: "center",
            gap: 5,
            flexWrap: "wrap",
          }}
        >
          {/* Which endpoint this payment was for. A run-funded agent emits
              one row for the run's up-front funding settlement plus one per
              tool call, all against the same tx id — without a label they
              read as duplicate charges, and an agent with several attached
              tool402 nodes gave no way to tell its payment rows apart. */}
          {payment.nodeName && (
            <span style={{ color: "var(--fg-dim)" }}>{payment.nodeName} ·</span>
          )}
          <span style={{ color: "var(--fg)" }}>
            {payment.amount
              ? `${parseFloat(payment.amount).toFixed(6)} paid`
              : "paid"}
          </span>
          {payment.txId && payment.explorerURL && (
            <>
              <span style={{ color: "var(--fg-dim)" }}>· in</span>
              <a
                href={payment.explorerURL}
                target="_blank"
                rel="noopener noreferrer"
                style={txLinkStyle}
                onClick={(e) => e.stopPropagation()}
              >
                {payment.txId.slice(0, 8)}…
              </a>
            </>
          )}
          {payment.outboundTxId && payment.outboundExplorerURL && (
            <>
              <span style={{ color: "var(--fg-dim)" }}>· out</span>
              <a
                href={payment.outboundExplorerURL}
                target="_blank"
                rel="noopener noreferrer"
                style={txLinkStyle}
                onClick={(e) => e.stopPropagation()}
              >
                {payment.outboundTxId.slice(0, 8)}…
              </a>
            </>
          )}
        </span>
      )}
      {text === "" ? (
        !payment && <span style={{ color: "var(--fg-dim)" }}>—</span>
      ) : collapsible ? (
        <>
          <button
            onClick={() => setExpanded((v) => !v)}
            style={{
              background: "none",
              border: "none",
              padding: 0,
              cursor: "pointer",
              color: "var(--accent)",
              fontFamily: "var(--font-mono)",
              fontSize: 10,
            }}
          >
            {expanded ? "▾" : "▸"} response · {formatSize(text.length)}
          </button>
          {expanded && (
            <pre
              style={{
                margin: "4px 0 0",
                padding: 8,
                maxHeight: 220,
                overflow: "auto",
                background: "var(--bg)",
                border: "1px solid var(--border)",
                borderRadius: 4,
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                lineHeight: 1.5,
                color: "var(--fg)",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}
            >
              {text}
            </pre>
          )}
        </>
      ) : (
        <span
          style={{
            color: "var(--fg)",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
          }}
        >
          {text}
        </span>
      )}
    </div>
  );
}

const txLinkStyle: React.CSSProperties = {
  color: "#E879F9",
  textDecoration: "underline",
  fontFamily: "var(--font-mono)",
  fontSize: 9.5,
  whiteSpace: "nowrap",
};

function nodeTypeColor(t: string): string {
  if (t === "agent") return "var(--accent)";
  if (t === "tool402") return "#E879F9";
  if (t === "action") return "#FB923C";
  return "var(--fg-dim)";
}
