"use client";
import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Logo, Pill, Hairline, IconSearch } from "@/components/ui";
import { useAuth } from "@/hooks/useAuth";
import { usage as usageApi } from "@/lib/api";
import {
  UsageRange, UsagePayload, EndpointUsage, UsageCategory,
} from "@/lib/types";
import { AreaChart } from "./AreaChart";
import { Donut, DonutSegment } from "./Donut";

const RANGES: UsageRange[] = ["24h", "7d", "30d"];

// x402 = accent, LLM = info, action = the orange already used in LogDrawer.
const CAT_COLOR: Record<UsageCategory, string> = {
  x402: "var(--accent)",
  llm: "var(--info)",
  action: "#FB923C",
};
// Endpoint type pill keeps the x402 magenta used elsewhere (tx links / tool402).
const TYPE_PILL: Record<UsageCategory, string> = {
  x402: "#E879F9",
  llm: "var(--info)",
  action: "#FB923C",
};
const CAT_LABEL: Record<UsageCategory, string> = { x402: "x402", llm: "LLM", action: "Actions" };

export function UsagePage() {
  const router = useRouter();
  const { signOut } = useAuth();

  const [range, setRange] = useState<UsageRange>("30d");
  const [data, setData] = useState<UsagePayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [scopedWf, setScopedWf] = useState<string | null>(null);

  // ?workflow=<id> deep-link filter (read without useSearchParams to avoid a
  // Suspense boundary requirement).
  useEffect(() => {
    if (typeof window === "undefined") return;
    const wf = new URLSearchParams(window.location.search).get("workflow");
    if (wf) setScopedWf(wf);
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    Promise.all([
      usageApi.summary(range),
      usageApi.timeseries(range),
      usageApi.byWorkflow(range),
      usageApi.byEndpoint(range),
      usageApi.settlements(18),
    ])
      .then(([summary, timeseries, byWorkflow, byEndpoint, settlements]) => {
        if (cancelled) return;
        setData({ summary, timeseries, byWorkflow, byEndpoint, settlements });
      })
      .catch(() => { if (!cancelled) setData(null); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [range]);

  const handleSignOut = async () => { await signOut(); router.push("/"); };

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", overflow: "hidden", background: "var(--bg)" }}>
      {/* Topbar */}
      <div style={{ height: 56, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", padding: "0 24px", display: "flex", alignItems: "center", gap: 14 }}>
        <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0 }}>
          <Logo size={18} />
        </button>
        <Hairline vertical length={22} />
        <button style={ghostBtnSm} onClick={() => router.push("/workflows")}>← Workflows</button>
        <Pill mono dot tone="ok">testnet</Pill>
        <div style={{ flex: 1 }} />
        <button style={{ ...ghostBtnSm, borderColor: "var(--accent-line)", color: "var(--accent)" }}>Usage</button>
        <button style={ghostBtnSm} onClick={() => router.push("/workflows")}>Credentials</button>
        <button style={ghostBtnSm} onClick={() => router.push("/workflows")}>Settings</button>
        <Hairline vertical length={22} />
        <button style={ghostBtnSm} onClick={handleSignOut}>Sign out</button>
        <div style={{ width: 28, height: 28, borderRadius: 999, background: "var(--accent)", color: "var(--accent-fg)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 700 }}>AC</div>
      </div>

      {/* Main */}
      <div style={{ flex: 1, overflow: "auto", background: "var(--bg)" }}>
        <div style={{ maxWidth: 1280, margin: "0 auto", padding: "36px 24px 80px" }}>
          {scopedWf && (
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 16, padding: "8px 12px", background: "var(--accent-soft)", border: "1px solid var(--accent-line)", borderRadius: "var(--r-2)", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)" }}>
              scoped to workflow · {scopedWf}
              <button onClick={() => setScopedWf(null)} style={{ marginLeft: "auto", background: "transparent", border: "none", color: "var(--accent)", cursor: "pointer", fontFamily: "var(--font-mono)", fontSize: 11, textDecoration: "underline" }}>clear</button>
            </div>
          )}

          {loading && !data ? (
            <div style={{ padding: 64, textAlign: "center", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>loading usage…</div>
          ) : !data ? (
            <div style={{ padding: 48, textAlign: "center", border: "1px dashed var(--border)", borderRadius: "var(--r-3)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              no usage yet — run a workflow to see spend here
            </div>
          ) : (
            <UsageBody data={data} range={range} onRangeChange={setRange} scopedWf={scopedWf} onOpenWorkflow={(id) => router.push(`/workflows/${id}`)} loading={loading} />
          )}
        </div>
      </div>
    </div>
  );
}

function UsageBody({ data, range, onRangeChange, scopedWf, onOpenWorkflow, loading }: {
  data: UsagePayload; range: UsageRange; onRangeChange: (r: UsageRange) => void;
  scopedWf: string | null;
  onOpenWorkflow: (id: string) => void; loading: boolean;
}) {
  const { timeseries, byWorkflow, byEndpoint, settlements } = data;

  const workflows = scopedWf ? byWorkflow.filter((w) => w.workflowId === scopedWf) : byWorkflow;

  // Cost-by-category from endpoint totals.
  const catTotals = useMemo(() => {
    const t: Record<UsageCategory, number> = { x402: 0, llm: 0, action: 0 };
    for (const e of byEndpoint) t[e.type] += e.totalAlgo;
    return t;
  }, [byEndpoint]);
  const catTotal = catTotals.x402 + catTotals.llm + catTotals.action;
  const segments: DonutSegment[] = (["x402", "llm", "action"] as UsageCategory[])
    .map((k) => ({ label: CAT_LABEL[k], value: catTotals[k], color: CAT_COLOR[k] }));

  const maxWfAlgo = Math.max(...workflows.map((x) => x.algo), 1e-9);

  return (
    <div style={{ opacity: loading ? 0.6 : 1, transition: "opacity .15s" }}>
      {/* Header row above the Endpoints table: credits left (left) mirrors the range selector (right).
          Keep the empty headspace above it — content starts low on the page. */}
      <div className="reveal reveal-delay-1" style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-end", gap: 16, flexWrap: "wrap", paddingTop: 60, marginBottom: 12 }}>
        <div style={{ display: "flex", alignItems: "flex-end", gap: 14, flexWrap: "wrap" }}>
          {(() => {
            const b = data.summary.budget;
            const left = b ? b.limit - b.used : null;
            const pctLeft = b && b.limit > 0 ? Math.max(0, Math.min(1, (b.limit - b.used) / b.limit)) : null;
            const tone = pctLeft == null ? "var(--accent)" : pctLeft < 0.1 ? "var(--danger)" : pctLeft < 0.25 ? "#FB923C" : "var(--accent)";
            return (
              <div style={{
                flex: "0 0 auto", minWidth: 300, maxWidth: "100%",
                background: "var(--bg-elev-1)", border: "1px solid var(--border)",
                borderRadius: "var(--r-3)", padding: "16px 20px",
                display: "flex", flexDirection: "column", gap: 8, position: "relative",
              }}>
                <button className="credit-topup" aria-label="Credit top-up">
                  <svg width="15" height="15" viewBox="0 0 15 15" fill="none" aria-hidden="true">
                    <path d="M7.5 2v11M2 7.5h11" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                  </svg>
                  <span className="credit-topup__tip">credits top-up</span>
                </button>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.12em", color: "var(--fg-dim)" }}>
                  credits left
                </span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 10 }}>
                  <span style={{ fontSize: 40, fontWeight: 500, lineHeight: 1, letterSpacing: "-0.02em", color: tone }}>
                    {left == null ? "…" : compactUsd(left)}
                  </span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 14, color: "var(--fg-muted)" }}>USD</span>
                </div>
                {pctLeft != null && (
                  <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <div style={{ flex: 1, height: 4, background: "var(--accent-soft)", borderRadius: 999, overflow: "hidden" }}>
                      <div style={{ height: "100%", width: `${pctLeft * 100}%`, background: tone, borderRadius: 999 }} />
                    </div>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", whiteSpace: "nowrap" }}>{Math.round(pctLeft * 100)}% left</span>
                  </div>
                )}
              </div>
            );
          })()}
        </div>
        <div style={{ display: "flex", alignItems: "flex-end", gap: 14, flexWrap: "wrap" }}>
          <RangeSelector range={range} onChange={onRangeChange} />
        </div>
      </div>

      {/* ④ Endpoints table — first */}
      <EndpointTable rows={byEndpoint} className="reveal reveal-delay-1" style={{ marginBottom: 16 }} />

      {/* ② Usage + Spend merged — two distinct-coloured lines, combined tooltip */}
      <Card className="reveal reveal-delay-2" style={{ marginBottom: 16 }}>
        <CardHead
          title="Usage & Spend over time"
          right={<Legend items={[
            { c: "var(--accent)", label: "Spend (USD)" },
            { c: "var(--warm)", label: "Usage (calls)" },
          ]} />}
        />
        <div style={{ padding: "4px 4px 0" }}>
          <AreaChart data={timeseries} algoUsd={ALGO_USD} />
        </div>
      </Card>

      {/* ③ Two-column */}
      <div className="usage-split reveal reveal-delay-3" style={{ marginBottom: 16 }}>
        {/* Workflows by spend */}
        <Card>
          <CardHead title="Workflows by spend" />
          <div style={{ padding: "4px 0 8px" }}>
            {workflows.length === 0 ? (
              <Empty text="no workflow spend in range" />
            ) : (
              workflows.slice(0, 6).map((w) => (
                <button key={w.workflowId} onClick={() => onOpenWorkflow(w.workflowId)} style={spendRowStyle}>
                  <span style={{ fontSize: 13, color: "var(--fg)", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: "0 0 40%", textAlign: "left" }}>{w.name}</span>
                  <span style={{ flex: 1, height: 6, background: "var(--accent-soft)", borderRadius: 999, overflow: "hidden" }}>
                    <span style={{ display: "block", height: "100%", width: `${(w.algo / maxWfAlgo) * 100}%`, background: "var(--accent)", borderRadius: 999 }} />
                  </span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--accent)", flex: "0 0 auto", minWidth: 78, textAlign: "right" }}>{usd(w.algo)} <span style={{ color: "var(--fg-dim)" }}>USD</span></span>
                </button>
              ))
            )}
          </div>
        </Card>

        {/* Cost by category */}
        <Card>
          <CardHead title="Cost by category" />
          <div style={{ display: "flex", alignItems: "center", gap: 24, padding: "12px 4px 8px", flexWrap: "wrap" }}>
            <Donut segments={segments} centerLabel={usd(catTotal)} centerSub="USD" />
            <div style={{ flex: 1, minWidth: 160, display: "flex", flexDirection: "column", gap: 12 }}>
              {segments.map((s) => (
                <div key={s.label} style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <span style={{ width: 10, height: 10, borderRadius: 3, background: s.color, flexShrink: 0 }} />
                  <span style={{ fontSize: 13, color: "var(--fg)" }}>{s.label}{s.label === "LLM" && <span style={{ color: "var(--fg-dim)" }}>*</span>}</span>
                  <span style={{ marginLeft: "auto", fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg)" }}>{usd(s.value)}</span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", minWidth: 38, textAlign: "right" }}>{catTotal > 0 ? Math.round((s.value / catTotal) * 100) : 0}%</span>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>

      {/* ⑤ Recent settlements — on-chain, kept in ALGO */}
      <Card style={{ marginBottom: 16 }}>
        <CardHead title="Recent settlements" right={<span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>on-chain · testnet</span>} />
        <div style={{ display: "grid", gridTemplateColumns: SETTLE_GRID, gap: 14, padding: "8px 10px", background: "var(--bg-elev-2)", borderRadius: "var(--r-2)", marginTop: 4, alignItems: "center" }}>
          <span style={hcell}>Endpoint</span>
          <span style={hcell}>Hash</span>
          <span style={hcell}>Workflow</span>
          <span style={{ ...hcell, textAlign: "right" }}>Amount</span>
          <span style={{ ...hcell, textAlign: "right" }}>Time</span>
        </div>
        <div style={{ padding: "2px 0" }}>
          {settlements.map((s, i) => (
            <div key={s.txId} style={{ display: "grid", gridTemplateColumns: SETTLE_GRID, gap: 14, alignItems: "center", padding: "11px 10px", borderBottom: i < settlements.length - 1 ? "1px solid var(--border-soft)" : "none", fontFamily: "var(--font-mono)", fontSize: 11 }}>
              <span style={{ color: "var(--fg-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{s.endpoint}</span>
              <a href={s.explorerURL} target="_blank" rel="noopener noreferrer" style={{ color: "#E879F9", textDecoration: "underline", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{s.txId.slice(0, 13)}…</a>
              <span style={{ color: "var(--fg-dim)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{s.workflowId}</span>
              <span style={{ color: "var(--fg)", textAlign: "right" }}>{s.amountAlgo.toFixed(6)} <span style={{ color: "var(--fg-dim)" }}>ALGO</span></span>
              <span style={{ color: "var(--fg-dim)", textAlign: "right" }}>{relTime(s.ts)}</span>
            </div>
          ))}
        </div>
      </Card>

      {/* ⑥ Footer note */}
      <p style={{ margin: "20px 4px 0", fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", letterSpacing: "0.02em" }}>
        settled on Algorand testnet · figures update ~every run · LLM costs (*) are estimated
      </p>
    </div>
  );
}

// ── Endpoints table ─────────────────────────────────────────────────────────
type SortKey = "endpoint" | "calls" | "unitPrice" | "totalAlgo" | "pctOfSpend" | "successRate" | "lastUsedAt";

const EP_GRID = "1.9fr 1.15fr 66px 66px 96px 108px 116px 78px 92px";
const SETTLE_GRID = "minmax(0,1.9fr) minmax(0,1.15fr) 140px 114px 108px"; // Endpoint · Hash · Workflow · Amount · Time

function EndpointTable({ rows, className, style }: { rows: EndpointUsage[]; className?: string; style?: React.CSSProperties }) {
  const [cat, setCat] = useState<"all" | UsageCategory>("all");
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<{ key: SortKey; dir: "asc" | "desc" }>({ key: "totalAlgo", dir: "desc" });

  const filtered = useMemo(() => {
    const ql = q.trim().toLowerCase();
    const out = rows.filter((r) => (cat === "all" || r.type === cat)
      && (!ql || r.endpoint.toLowerCase().includes(ql) || r.provider.toLowerCase().includes(ql) || r.host.toLowerCase().includes(ql)));
    out.sort((a, b) => {
      const av = a[sort.key], bv = b[sort.key];
      let cmp: number;
      if (typeof av === "string" && typeof bv === "string") cmp = av.localeCompare(bv);
      else {
        const an = av == null ? -Infinity : Number(av);
        const bn = bv == null ? -Infinity : Number(bv);
        cmp = an - bn;
      }
      return sort.dir === "asc" ? cmp : -cmp;
    });
    return out;
  }, [rows, cat, q, sort]);

  const toggle = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === "asc" ? "desc" : "asc" } : { key, dir: "desc" }));

  return (
    <Card className={className} style={style}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "2px 2px 14px", flexWrap: "wrap" }}>
        <div style={{ fontSize: 14, fontWeight: 500, color: "var(--fg)" }}>Endpoints &amp; APIs</div>
        <div style={{ display: "flex", gap: 2, background: "var(--bg-elev-2)", padding: 3, borderRadius: "var(--r-2)", border: "1px solid var(--border)" }}>
          {(["all", "x402", "llm", "action"] as const).map((c) => (
            <button key={c} onClick={() => setCat(c)} style={{ border: "none", background: cat === c ? "var(--bg-elev-3)" : "transparent", color: cat === c ? "var(--fg)" : "var(--fg-muted)", padding: "5px 11px", fontSize: 12, fontWeight: 500, borderRadius: 5, cursor: "pointer", fontFamily: "var(--font-sans)", textTransform: "capitalize" }}>
              {c === "llm" ? "LLM" : c}
            </button>
          ))}
        </div>
        <div style={{ flex: 1 }} />
        <div style={{ position: "relative" }}>
          <span style={{ position: "absolute", left: 10, top: 9, color: "var(--fg-dim)" }}><IconSearch size={12} /></span>
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search endpoint / provider…"
            style={{ height: 32, paddingLeft: 30, paddingRight: 12, width: 240, maxWidth: "100%", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontFamily: "var(--font-sans)", fontSize: 12, outline: "none" }} />
        </div>
      </div>

      <div style={{ overflowX: "auto" }}>
        <div style={{ minWidth: 960 }}>
          <div style={{ display: "grid", gridTemplateColumns: EP_GRID, gap: 10, padding: "8px 10px", background: "var(--bg-elev-2)", borderRadius: "var(--r-2)", alignItems: "center" }}>
            <Th k="endpoint" sort={sort} onToggle={toggle}>Endpoint</Th>
            <span style={hcell}>Provider</span>
            <span style={hcell}>Type</span>
            <Th k="calls" align="right" sort={sort} onToggle={toggle}>Calls</Th>
            <Th k="unitPrice" align="right" sort={sort} onToggle={toggle}>Unit price</Th>
            <Th k="totalAlgo" align="right" sort={sort} onToggle={toggle}>Total</Th>
            <Th k="pctOfSpend" align="right" sort={sort} onToggle={toggle}>% spend</Th>
            <Th k="successRate" align="right" sort={sort} onToggle={toggle}>Success</Th>
            <Th k="lastUsedAt" align="right" sort={sort} onToggle={toggle}>Last used</Th>
          </div>

          {filtered.length === 0 ? (
            <Empty text="no endpoints match" />
          ) : filtered.map((r) => (
            <div key={r.endpoint} style={{ display: "grid", gridTemplateColumns: EP_GRID, gap: 10, padding: "12px 10px", alignItems: "center", borderBottom: "1px solid var(--border-soft)" }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 13, color: "var(--fg)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{r.endpoint}</div>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{r.host}</div>
              </div>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{r.provider}</span>
              <TypeTag type={r.type} />
              <span style={numCell}>{r.calls.toLocaleString()}</span>
              <span style={{ ...numCell, color: "var(--fg-muted)" }}>{r.unitPrice != null ? `${trim(r.unitPrice)}/${r.unit.split(" ")[0]}` : `—`}</span>
              <span style={{ ...numCell, color: "var(--accent)" }}>{algo(r.totalAlgo, 3)}{r.type === "llm" && "*"}</span>
              <span style={{ ...numCell, display: "flex", alignItems: "center", gap: 6, justifyContent: "flex-end" }}>
                <span style={{ width: 34, height: 5, background: "var(--accent-soft)", borderRadius: 999, overflow: "hidden", flexShrink: 0 }}>
                  <span style={{ display: "block", height: "100%", width: `${Math.min(100, r.pctOfSpend)}%`, background: "var(--accent)" }} />
                </span>
                {r.pctOfSpend}%
              </span>
              <span style={{ ...numCell, color: r.successRate == null ? "var(--fg-dim)" : r.successRate < 90 ? "var(--danger)" : r.successRate < 98 ? "var(--warm)" : "var(--fg-muted)" }}>
                {r.successRate == null ? "—" : `${r.successRate}%`}
              </span>
              <span style={{ ...numCell, color: "var(--fg-muted)" }}>{relTime(r.lastUsedAt)}</span>
            </div>
          ))}
        </div>
      </div>
    </Card>
  );
}

// Sortable column header — declared at module scope (not inside EndpointTable's
// render) so it isn't recreated every render. Sort state + toggle come via props.
function Th({ k, children, align = "left", sort, onToggle }: {
  k: SortKey; children: React.ReactNode; align?: "left" | "right";
  sort: { key: SortKey; dir: "asc" | "desc" }; onToggle: (k: SortKey) => void;
}) {
  return (
    <button onClick={() => onToggle(k)} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0, color: sort.key === k ? "var(--fg-muted)" : "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", display: "inline-flex", gap: 3, justifyContent: align === "right" ? "flex-end" : "flex-start" }}>
      {children}{sort.key === k ? (sort.dir === "asc" ? "▴" : "▾") : ""}
    </button>
  );
}

// ── Small building blocks ───────────────────────────────────────────────────
function RangeSelector({ range, onChange }: { range: UsageRange; onChange: (r: UsageRange) => void }) {
  return (
    <div style={{ display: "flex", gap: 2, background: "var(--bg-elev-1)", padding: 3, borderRadius: "var(--r-2)", border: "1px solid var(--border)" }}>
      {RANGES.map((r) => (
        <button key={r} onClick={() => onChange(r)}
          style={{ border: "none", background: range === r ? "var(--bg-elev-3)" : "transparent", color: range === r ? "var(--fg)" : "var(--fg-muted)", padding: "6px 14px", fontSize: 12, fontWeight: 500, borderRadius: 5, cursor: "pointer", fontFamily: "var(--font-mono)" }}>
          {r}
        </button>
      ))}
    </div>
  );
}

function TypeTag({ type }: { type: UsageCategory }) {
  const c = TYPE_PILL[type];
  return (
    <span style={{ justifySelf: "start", display: "inline-flex", alignItems: "center", height: 20, padding: "0 8px", borderRadius: 999, border: `1px solid ${c}55`, background: `${c}1A`, color: c, fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.04em" }}>
      {type === "llm" ? "LLM" : type}
    </span>
  );
}

function Card({ children, className, style }: { children: React.ReactNode; className?: string; style?: React.CSSProperties }) {
  return <div className={className} style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: 16, ...style }}>{children}</div>;
}

function CardHead({ title, right }: { title: string; right?: React.ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 6 }}>
      <div style={{ fontSize: 14, fontWeight: 500, color: "var(--fg)" }}>{title}</div>
      {right}
    </div>
  );
}

function Legend({ items }: { items: { c: string; label: string }[] }) {
  return (
    <div style={{ display: "flex", gap: 14 }}>
      {items.map((i) => (
        <span key={i.label} style={{ display: "inline-flex", alignItems: "center", gap: 6, fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)" }}>
          <span style={{ width: 8, height: 8, borderRadius: 999, background: i.c }} />{i.label}
        </span>
      ))}
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return <div style={{ padding: 28, textAlign: "center", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 11 }}>{text}</div>;
}

// ── formatting helpers ──────────────────────────────────────────────────────
// On-chain amounts are ALGO; user-facing credit/spend is shown in USD.
// Single display rate — swap for a live oracle when available.
const ALGO_USD = 0.17;
function usd(algoAmount: number, dp = 2) {
  return (algoAmount * ALGO_USD).toLocaleString("en", { minimumFractionDigits: dp, maximumFractionDigits: dp });
}
// Compact USD for the credit balance — keeps large figures small (100K, 50, 2.3M).
function compactUsd(algoAmount: number) {
  return Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(algoAmount * ALGO_USD);
}
function algo(n: number, dp: number) {
  return n.toLocaleString("en", { minimumFractionDigits: dp, maximumFractionDigits: dp });
}
function trim(n: number) {
  return n.toLocaleString("en", { minimumFractionDigits: 0, maximumFractionDigits: 4 });
}
function relTime(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

const hcell: React.CSSProperties = { fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)" };
const numCell: React.CSSProperties = { fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg)", textAlign: "right" };
const spendRowStyle: React.CSSProperties = {
  display: "flex", alignItems: "center", gap: 12, width: "100%", padding: "9px 4px",
  background: "transparent", border: "none", borderBottom: "1px solid var(--border-soft)", cursor: "pointer",
};
const ghostBtnSm: React.CSSProperties = {
  height: 28, padding: "0 10px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
