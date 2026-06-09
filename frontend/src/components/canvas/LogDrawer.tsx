"use client";
import { useEffect, useRef, useState } from "react";
import { Pill } from "@/components/ui";

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
}

const _CONFIGURED = process.env.NEXT_PUBLIC_API_URL ?? "";
const BASE = _CONFIGURED && typeof window !== "undefined" ? "/api" : _CONFIGURED;

export function LogDrawer({ open, onToggle, runId, running, onRunComplete }: LogDrawerProps) {
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [elapsed, setElapsed] = useState<number | null>(null);
  const [done, setDone] = useState(false);
  const esRef = useRef<EventSource | null>(null);
  const startRef = useRef<number | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // Reset and connect SSE when runId changes
  useEffect(() => {
    if (!runId) return;
    setLogs([]);
    setDone(false);
    setElapsed(null);
    startRef.current = Date.now();

    // Start elapsed timer
    timerRef.current = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startRef.current!) / 100) / 10);
    }, 100);

    const url = BASE
      ? `${BASE}/runs/${runId}/stream`
      : null;

    if (!url) return;

    // withCredentials sends the HttpOnly auth cookie automatically.
    const es = new EventSource(url, { withCredentials: true });
    esRef.current = es;

    es.addEventListener("log", (e) => {
      try {
        const ev: LogEvent = JSON.parse((e as MessageEvent).data);
        setLogs((prev) => {
          // Replace running entry for same nodeId, or append
          const idx = prev.findIndex((l) => l.nodeId === ev.nodeId && l.status === "running");
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = ev;
            return next;
          }
          return [...prev, ev];
        });
      } catch { /* ignore parse errors */ }
    });

    es.addEventListener("done", () => {
      clearInterval(timerRef.current!);
      setDone(true);
      onRunComplete();
      es.close();
    });

    es.onerror = () => {
      clearInterval(timerRef.current!);
      setDone(true);
      onRunComplete();
      es.close();
    };

    return () => {
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

  // Auto-scroll to bottom on new logs
  useEffect(() => {
    if (open) bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs, open]);

  const statusColor = (s: LogEvent["status"]) => {
    if (s === "success") return "var(--accent)";
    if (s === "failed")  return "#F87171";
    if (s === "stopped") return "#FB923C";
    return "var(--fg-dim)";
  };

  const statusLabel = (s: LogEvent["status"]) => {
    if (s === "success") return "OK";
    if (s === "failed")  return "ERR";
    if (s === "stopped") return "STP";
    return "RUN";
  };

  const outputPreview = (output: unknown): string => {
    if (output === null || output === undefined) return "—";
    if (typeof output === "string") return output;
    if (typeof output === "object" && output !== null) {
      const m = output as Record<string, unknown>;
      if (typeof m.message === "string") return m.message;
    }
    try { return JSON.stringify(output); }
    catch { return String(output); }
  };

  const elapsedStr = (elapsed ?? 0).toFixed(1);
  const headerPill = runId
    ? (done ? `run · ${runId.slice(0, 8)} · ${elapsedStr}s` : running ? `running · ${elapsedStr}s` : `run · ${runId.slice(0, 8)}`)
    : "console";

  const pillTone = done ? "ok" : running ? "warm" : "default";

  return (
    <div style={{
      position: "absolute", left: 0, right: 0, bottom: 0,
      background: "var(--bg-elev-1)", borderTop: "1px solid var(--border)",
      height: open ? 240 : 32,
      transition: "height .2s cubic-bezier(.2,.8,.2,1)",
      display: "flex", flexDirection: "column",
      zIndex: 5,
    }}>
      {/* Header bar */}
      <div onClick={onToggle} style={{ height: 32, padding: "0 14px", display: "flex", alignItems: "center", justifyContent: "space-between", cursor: "pointer", borderBottom: open ? "1px solid var(--border)" : "none", flexShrink: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)", textTransform: "uppercase", letterSpacing: "0.08em" }}>console</span>
          <Pill mono tone={pillTone} dot={running}>{headerPill}</Pill>
          {logs.length > 0 && (
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>{logs.length} step{logs.length !== 1 ? "s" : ""}</span>
          )}
        </div>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
          {open ? "▾ collapse" : "▴ expand"}
        </span>
      </div>

      {/* Log lines */}
      {open && (
        <div style={{ flex: 1, overflow: "auto", padding: "6px 14px 10px", fontFamily: "var(--font-mono)", fontSize: 11, lineHeight: 1.7 }}>
          {logs.length === 0 && !running && !runId && (
            <div style={{ color: "var(--fg-dim)", paddingTop: 8 }}>run a workflow to see execution logs here.</div>
          )}
          {logs.length === 0 && running && (
            <div style={{ color: "var(--fg-dim)", paddingTop: 8 }}>waiting for first node…</div>
          )}
          {logs.map((l, i) => (
            <div key={i} style={{ display: "grid", gridTemplateColumns: "52px 34px 110px 1fr", gap: 10, alignItems: "baseline", borderBottom: "1px solid var(--border-soft)", padding: "3px 0" }}>
              <span style={{ color: "var(--fg-dim)", fontSize: 9.5 }}>
                {new Date(l.ts).toLocaleTimeString("en", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })}
              </span>
              <span style={{ color: statusColor(l.status), fontWeight: 600, fontSize: 9.5 }}>{statusLabel(l.status)}</span>
              <span style={{ color: "var(--fg-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                <span style={{ color: nodeTypeColor(l.nodeType) }}>{l.nodeType}</span>
                {l.durationMs > 0 && <span style={{ color: "var(--fg-dim)" }}> · {l.durationMs}ms</span>}
              </span>
              {isX402Payment(l.output) ? (
                <span style={{ display: "flex", alignItems: "center", gap: 5, overflow: "hidden" }}>
                  <span style={{ color: "var(--fg)" }}>
                    {l.output.amount ? `${parseFloat(l.output.amount).toFixed(6)} ALGO` : "paid"}
                  </span>
                  {l.output.txId && l.output.explorerURL && (
                    <>
                      <span style={{ color: "var(--fg-dim)" }}>·</span>
                      <a
                        href={l.output.explorerURL}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{ color: "#E879F9", textDecoration: "underline", fontFamily: "var(--font-mono)", fontSize: 9.5, whiteSpace: "nowrap" }}
                        onClick={(e) => e.stopPropagation()}
                      >
                        {l.output.txId.slice(0, 8)}…
                      </a>
                    </>
                  )}
                </span>
              ) : (
                <span style={{ color: "var(--fg)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{outputPreview(l.output)}</span>
              )}
            </div>
          ))}
          {done && (
            <div style={{ color: "var(--accent)", paddingTop: 6, fontSize: 10 }}>
              ✓ run complete · {(elapsed ?? 0).toFixed(1)}s · {logs.filter(l => l.status === "success").length}/{logs.length} nodes succeeded
            </div>
          )}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}

function nodeTypeColor(t: string): string {
  if (t === "agent")   return "var(--accent)";
  if (t === "tool402") return "#E879F9";
  if (t === "action")  return "#FB923C";
  return "var(--fg-dim)";
}
