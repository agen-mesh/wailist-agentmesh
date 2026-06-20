"use client";
import { useState, useMemo, useEffect, useCallback } from "react";
import { useMobile } from "@/hooks/useMobile";
import { useRouter } from "next/navigation";
import { Logo, Pill, Tag, Hairline, IconSearch, IconGrid } from "@/components/ui";
import { Workflow } from "@/lib/types";
import { useAuth } from "@/hooks/useAuth";
import { workflows as workflowsApi, auth as authApi } from "@/lib/api";
import { WORKFLOW_TEMPLATES, WorkflowTemplate } from "@/lib/data";

export function WorkflowsPage() {
  const router = useRouter();
  const isMobile = useMobile();
  const { signOut } = useAuth();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("all");
  const [view, setView] = useState<"rows" | "grid">("rows");
  const [wfList, setWfList] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [spawning, setSpawning] = useState<string | null>(null);

  useEffect(() => {
    workflowsApi.list()
      .then(setWfList)
      .catch(() => setWfList([]))
      .finally(() => setLoading(false));
  }, []);

  const filtered = useMemo(() => {
    return wfList.filter((wf) => {
      const matchesQ = !q || wf.name?.toLowerCase().includes(q.toLowerCase()) || wf.tags?.join(" ").includes(q.toLowerCase());
      const matchesS = status === "all" || wf.status === status;
      return matchesQ && matchesS;
    });
  }, [wfList, q, status]);

  const handleNewWorkflow = useCallback(async () => {
    if (creating) return;
    setCreating(true);
    try {
      const wf = await workflowsApi.create("Untitled workflow");
      router.push(`/workflows/${wf.id}`);
    } catch {
      setCreating(false);
    }
  }, [creating, router]);

  const handleUseTemplate = useCallback(async (tpl: WorkflowTemplate) => {
    if (spawning) return;
    setSpawning(tpl.id);
    try {
      const wf = await workflowsApi.createFromTemplate(tpl.name, tpl.nodes, tpl.edges);
      router.push(`/workflows/${wf.id}`);
    } catch {
      setSpawning(null);
    }
  }, [spawning, router]);

  const handleSignOut = async () => {
    await signOut();
    router.push("/");
  };

  const handleDelete = useCallback(async (id: string) => {
    if (!confirm("Delete this workflow? This cannot be undone.")) return;
    await workflowsApi.delete(id);
    setWfList((prev) => prev.filter((w) => w.id !== id));
  }, []);

  const activeCount = wfList.filter((w) => w.status === "active").length;

  const [credits, setCredits] = useState<number | null>(null);
  const [toppingUp, setToppingUp] = useState(false);

  useEffect(() => {
    authApi.me().then((u) => setCredits(u.credits ?? 0)).catch(() => {});
  }, []);

  const handleTopup = async () => {
    setToppingUp(true);
    try {
      const res = await authApi.topup();
      setCredits(res.credits);
    } finally {
      setToppingUp(false);
    }
  };

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", overflow: "hidden", background: "var(--bg)" }}>
      {/* Topbar */}
      <div style={{ height: 56, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", padding: "0 16px", display: "flex", alignItems: "center", gap: isMobile ? 10 : 14 }}>
        <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0 }}>
          <Logo size={18} />
        </button>
        {!isMobile && <Hairline vertical length={22} />}
        {!isMobile && <button onClick={() => router.push("/marketplace")} style={navBtnStyle}>Marketplace</button>}
        {!isMobile && <button onClick={() => router.push("/billing")} style={navBtnStyle}>Billing</button>}
        {!isMobile && <Hairline vertical length={22} />}
        {!isMobile && <button style={ghostBtnSm}>Acme Capital ▾</button>}
        {!isMobile && <Pill mono dot tone="ok">testnet</Pill>}
        <div style={{ flex: 1 }} />
        {!isMobile && <Hairline vertical length={22} />}
        <button style={ghostBtnSm} onClick={handleSignOut}>Sign out</button>
        <div style={{ width: 28, height: 28, borderRadius: 999, background: "var(--accent)", color: "var(--accent-fg)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 700 }}>AC</div>
      </div>

      {/* Main */}
      <div style={{ flex: 1, overflow: "auto", background: "var(--bg)" }}>
        <div style={{ maxWidth: 1280, margin: "0 auto", padding: isMobile ? "24px 16px 60px" : "36px 24px 80px" }}>
          {/* Header */}
          <div style={{ display: "flex", flexDirection: isMobile ? "column" : "row", alignItems: isMobile ? "flex-start" : "flex-end", justifyContent: "space-between", gap: isMobile ? 16 : 0, marginBottom: 24 }}>
            <div>
              <Tag>your workspace</Tag>
              <h1 style={{ margin: "12px 0 4px", fontSize: isMobile ? 28 : 36, fontWeight: 500, letterSpacing: "-0.025em" }}>Workflows</h1>
              <p style={{ margin: 0, color: "var(--fg-muted)", fontSize: 14 }}>Design, deploy, and monitor agent pipelines.</p>
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
              {/* Credits widget */}
              <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "0 14px", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", height: 36 }}>
                <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>credits left</span>
                  <span style={{ fontFamily: "var(--font-sans)", fontSize: 13, fontWeight: 600, color: "var(--accent)" }}>
                    {credits == null ? "…" : `$${credits.toFixed(2)}`}
                  </span>
                </div>
                <button onClick={handleTopup} disabled={toppingUp} style={{ height: 24, padding: "0 10px", fontSize: 11, fontWeight: 500, background: "rgba(167,139,250,0.12)", border: "1px solid rgba(167,139,250,0.3)", borderRadius: "var(--r-1)", color: "var(--accent)", cursor: "pointer", fontFamily: "var(--font-sans)", opacity: toppingUp ? 0.6 : 1 }}>
                  {toppingUp ? "…" : "Top up +$10"}
                </button>
              </div>
              <button style={ghostBtn}>Import</button>
              <button onClick={handleNewWorkflow} disabled={creating} style={{ ...primaryBtn, opacity: creating ? 0.6 : 1 }}>
                {creating ? "Creating…" : "+ New workflow"}
              </button>
            </div>
          </div>

          {/* KPI row */}
          <div style={{ display: "grid", gridTemplateColumns: isMobile ? "repeat(2, 1fr)" : "repeat(4, 1fr)", gap: 12, marginBottom: 20 }}>
            <KpiCard label="Active workflows" value={loading ? "…" : activeCount} sub={loading ? "" : `of ${wfList.length} total`} />
            <KpiCard label="Agents deployed" value="—" sub="deploy a workflow" />
            <KpiCard label="Spend · 30d" value="—" unit="ALGO" sub="run a workflow" />
            <KpiCard label="Runs · 30d" value="—" sub="no runs yet" tone="ok" />
          </div>

          {/* Controls */}
          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 12 }}>
            <div style={{ position: "relative", flex: 1, minWidth: 180, maxWidth: 360 }}>
              <span style={{ position: "absolute", left: 12, top: 12, color: "var(--fg-dim)" }}><IconSearch size={12} /></span>
              <input
                style={{ height: 36, paddingLeft: 32, paddingRight: 12, width: "100%", background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontFamily: "var(--font-sans)", fontSize: 13, outline: "none" }}
                placeholder="Search workflows, tags…"
                value={q} onChange={(e) => setQ(e.target.value)}
              />
            </div>
            <div style={{ display: "flex", gap: 2, background: "var(--bg-elev-1)", padding: 3, borderRadius: "var(--r-2)", border: "1px solid var(--border)" }}>
              {["all", "active", "paused", "draft"].map((s) => (
                <button key={s} onClick={() => setStatus(s)}
                  style={{ border: "none", background: status === s ? "var(--bg-elev-3)" : "transparent", color: status === s ? "var(--fg)" : "var(--fg-muted)", padding: "6px 12px", fontSize: 12, fontWeight: 500, borderRadius: 5, cursor: "pointer", textTransform: "capitalize", fontFamily: "var(--font-sans)" }}>
                  {s}
                </button>
              ))}
            </div>
            <div style={{ flex: 1 }} />
            <div style={{ display: "flex", gap: 2, background: "var(--bg-elev-1)", padding: 3, borderRadius: "var(--r-2)", border: "1px solid var(--border)" }}>
              <button onClick={() => setView("rows")} style={{ ...ghostBtnSm, height: 26, background: view === "rows" ? "var(--bg-elev-3)" : "transparent", border: "none" }}>☰ Rows</button>
              <button onClick={() => setView("grid")} style={{ ...ghostBtnSm, height: 26, background: view === "grid" ? "var(--bg-elev-3)" : "transparent", border: "none", display: "flex", alignItems: "center", gap: 4 }}>
                <IconGrid size={11} /> Grid
              </button>
            </div>
          </div>

          {/* List */}
          {loading ? (
            <div style={{ padding: 48, textAlign: "center", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              loading workflows…
            </div>
          ) : (isMobile || view === "grid") ? (
            <WorkflowGrid items={filtered} onOpen={(id) => router.push(`/workflows/${id}`)} />
          ) : (
            <WorkflowRows items={filtered} onOpen={(id) => router.push(`/workflows/${id}`)} onDelete={handleDelete} />
          )}

          {!loading && filtered.length === 0 && wfList.length === 0 && (
            <TemplatesSection templates={WORKFLOW_TEMPLATES} spawning={spawning} onUse={handleUseTemplate} />
          )}
          {!loading && filtered.length === 0 && wfList.length > 0 && (
            <div style={{ padding: 48, textAlign: "center", border: "1px dashed var(--border)", borderRadius: "var(--r-3)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              no workflows match
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function KpiCard({ label, value, unit, sub, tone }: { label: string; value: string | number; unit?: string; sub?: string; tone?: string }) {
  return (
    <div style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: 16 }}>
      <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 10 }}>{label}</div>
      <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
        <span style={{ fontSize: 26, fontWeight: 500, letterSpacing: "-0.02em", color: tone === "ok" ? "var(--accent)" : "var(--fg)" }}>{value}</span>
        {unit && <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-muted)" }}>{unit}</span>}
      </div>
      {sub && <div style={{ marginTop: 4, fontSize: 11, color: "var(--fg-muted)" }}>{sub}</div>}
    </div>
  );
}

function StatusBadge({ status }: { status?: string }) {
  const map: Record<string, { tone: "ok" | "warm" | "default"; label: string }> = {
    active: { tone: "ok",      label: "Active" },
    paused: { tone: "warm",    label: "Paused" },
    draft:  { tone: "default", label: "Draft" },
  };
  const s = map[status ?? "draft"] ?? map.draft;
  return <Pill tone={s.tone} dot mono>{s.label}</Pill>;
}

function WorkflowIcon({ name }: { name: string }) {
  const seed = name.charCodeAt(0) + name.length;
  const dotCount = 3 + (seed % 3);
  const dots = Array.from({ length: dotCount }, (_, i) => {
    const a = (i / dotCount) * Math.PI * 2 + seed * 0.3;
    return { x: 14 + Math.cos(a) * 8, y: 14 + Math.sin(a) * 8 };
  });
  return (
    <div style={{ width: 36, height: 36, borderRadius: 8, background: "var(--bg-elev-3)", border: "1px solid var(--border-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
      <svg width="28" height="28" viewBox="0 0 28 28">
        {dots.map((d, i) => dots.map((d2, j) => (
          j > i ? <line key={`${i}_${j}`} x1={d.x} y1={d.y} x2={d2.x} y2={d2.y} stroke="var(--fg-dim)" strokeWidth="0.5" /> : null
        )))}
        {dots.map((d, i) => (
          <circle key={i} cx={d.x} cy={d.y} r="2" fill={i === 0 ? "var(--accent)" : "var(--fg-muted)"} />
        ))}
      </svg>
    </div>
  );
}

function WorkflowRows({ items, onOpen, onDelete }: { items: Workflow[]; onOpen: (id: string) => void; onDelete: (id: string) => void }) {
  const [openMenu, setOpenMenu] = useState<string | null>(null);

  // Close menu on outside click
  useEffect(() => {
    if (!openMenu) return;
    const close = () => setOpenMenu(null);
    window.addEventListener("click", close);
    return () => window.removeEventListener("click", close);
  }, [openMenu]);

  return (
    <div style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", overflow: "hidden" }}>
      <div style={{ display: "grid", gridTemplateColumns: "1.6fr 100px 80px 110px 130px 160px 80px", gap: 12, padding: "10px 16px", background: "var(--bg-elev-2)", borderBottom: "1px solid var(--border)", fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)" }}>
        <span>Name</span><span>Status</span><span>Agents</span><span>Runs · 30d</span><span>Spend · 30d</span><span>Updated</span><span></span>
      </div>
      {items.map((wf, i) => (
        <div key={wf.id} onClick={() => onOpen(wf.id)}
          style={{ display: "grid", gridTemplateColumns: "1.6fr 100px 80px 110px 130px 160px 80px", gap: 12, padding: "14px 16px", alignItems: "center", borderBottom: i < items.length - 1 ? "1px solid var(--border-soft)" : "none", cursor: "pointer", transition: "background .12s" }}
          onMouseEnter={(e) => (e.currentTarget.style.background = "var(--bg-elev-2)")}
          onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0 }}>
            <WorkflowIcon name={wf.name ?? ""} />
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 14, fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{wf.name}</div>
              <div style={{ display: "flex", gap: 5, marginTop: 4 }}>
                {wf.tags?.map((t) => <span key={t} style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>#{t}</span>)}
              </div>
            </div>
          </div>
          <StatusBadge status={wf.status} />
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{wf.agents ?? wf.nodes?.filter(n => n.type === "agent").length ?? 0}</span>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-muted)" }}>{wf.runs?.toLocaleString() ?? "—"}</span>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--accent)" }}>{wf.spend ?? "—"}{wf.spend && <span style={{ color: "var(--fg-dim)" }}> ALGO</span>}</span>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>{fmtDate(wf.updatedAt ?? wf.updated)}</span>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 4, position: "relative" }}>
            <button style={ghostBtnSm} onClick={(e) => { e.stopPropagation(); onOpen(wf.id); }}>Open</button>
            <button
              style={{ ...ghostBtnSm, width: 28, padding: 0, justifyContent: "center" }}
              onClick={(e) => { e.stopPropagation(); setOpenMenu(openMenu === wf.id ? null : wf.id); }}
            >⋯</button>
            {openMenu === wf.id && (
              <div
                onClick={(e) => e.stopPropagation()}
                style={{ position: "absolute", top: "100%", right: 0, marginTop: 4, background: "var(--bg-elev-3)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", zIndex: 50, minWidth: 140, boxShadow: "0 4px 16px rgba(0,0,0,0.4)", overflow: "hidden" }}
              >
                <button
                  onClick={() => { setOpenMenu(null); onDelete(wf.id); }}
                  style={{ display: "block", width: "100%", textAlign: "left", padding: "9px 14px", background: "transparent", border: "none", color: "#f87171", fontSize: 13, cursor: "pointer", fontFamily: "var(--font-sans)" }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = "rgba(248,113,113,0.08)")}
                  onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                >
                  Delete workflow
                </button>
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

function WorkflowGrid({ items, onOpen }: { items: Workflow[]; onOpen: (id: string) => void }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
      {items.map((wf) => (
        <div key={wf.id} onClick={() => onOpen(wf.id)}
          style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: 16, cursor: "pointer", transition: "border-color .15s, transform .15s" }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "var(--border-strong)"; (e.currentTarget as HTMLElement).style.transform = "translateY(-2px)"; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "var(--border)"; (e.currentTarget as HTMLElement).style.transform = "translateY(0)"; }}
        >
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
            <WorkflowIcon name={wf.name ?? ""} />
            <StatusBadge status={wf.status} />
          </div>
          <div style={{ marginTop: 16, fontSize: 16, fontWeight: 500, letterSpacing: "-0.015em" }}>{wf.name}</div>
          <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
            {wf.tags?.map((t) => <span key={t} style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>#{t}</span>)}
          </div>
          <div style={{ marginTop: 16, paddingTop: 12, borderTop: "1px solid var(--border-soft)", display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 8, fontFamily: "var(--font-mono)", fontSize: 11 }}>
            {[{ label: "Agents", val: String(wf.agents ?? wf.nodes?.filter(n => n.type === "agent").length ?? 0) }, { label: "Runs", val: wf.runs?.toLocaleString() ?? "—" }, { label: "Spend", val: wf.spend ?? "—", accent: true }].map((s) => (
              <div key={s.label}>
                <div style={{ color: "var(--fg-dim)", fontSize: 9, textTransform: "uppercase", letterSpacing: "0.06em" }}>{s.label}</div>
                <div style={{ color: s.accent ? "var(--accent)" : "var(--fg)", marginTop: 2 }}>{s.val}</div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Templates ──────────────────────────────────────────────────────────────

function TemplatesSection({ templates, spawning, onUse }: {
  templates: WorkflowTemplate[];
  spawning: string | null;
  onUse: (t: WorkflowTemplate) => void;
}) {
  return (
    <div>
      <div style={{ marginBottom: 20 }}>
        <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 6 }}>quick start</div>
        <div style={{ fontSize: 22, fontWeight: 500, letterSpacing: "-0.02em", color: "var(--fg)", marginBottom: 4 }}>Start from a template</div>
        <p style={{ margin: 0, fontSize: 13, color: "var(--fg-muted)" }}>Pre-built workflows you can customise and run in minutes.</p>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
        {templates.map((tpl) => (
          <TemplateCard key={tpl.id} tpl={tpl} loading={spawning === tpl.id} disabled={spawning !== null} onUse={() => onUse(tpl)} />
        ))}
      </div>
    </div>
  );
}

const NODE_TYPE_COLOR: Record<string, { bg: string; fg: string }> = {
  trigger:  { bg: "var(--bg-elev-3)",          fg: "var(--fg-muted)" },
  agent:    { bg: "rgba(167,139,250,0.12)",     fg: "var(--accent)" },
  provider: { bg: "rgba(167,139,250,0.08)",     fg: "var(--accent)" },
  tool:     { bg: "var(--bg-elev-3)",          fg: "var(--fg-muted)" },
  tool402:  { bg: "rgba(232,121,249,0.10)",     fg: "#E879F9" },
  action:   { bg: "rgba(255,181,71,0.10)",      fg: "var(--warm)" },
  end:      { bg: "var(--bg-elev-3)",          fg: "var(--fg-dim)" },
};

function TemplateCard({ tpl, loading, disabled, onUse }: {
  tpl: WorkflowTemplate;
  loading: boolean;
  disabled: boolean;
  onUse: () => void;
}) {
  const [hovered, setHovered] = useState(false);
  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        background: "var(--bg-elev-1)",
        border: `1px solid ${hovered ? "var(--accent-line)" : "var(--border)"}`,
        borderRadius: "var(--r-3)",
        padding: 20,
        display: "flex",
        flexDirection: "column",
        gap: 14,
        transition: "border-color 0.15s",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "var(--bg-elev-3)", border: "1px solid var(--border-strong)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 20, flexShrink: 0 }}>
          {tpl.icon}
        </div>
        <div>
          <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>{tpl.name}</div>
          <div style={{ display: "flex", gap: 5, marginTop: 3 }}>
            {tpl.tags.map((t) => (
              <span key={t} style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.05em" }}>#{t}</span>
            ))}
          </div>
        </div>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65, flex: 1 }}>{tpl.description}</p>

      <div style={{ display: "flex", alignItems: "center", gap: 4, flexWrap: "wrap" }}>
        {tpl.previewNodes.map((n, i) => {
          const c = NODE_TYPE_COLOR[n.type] ?? NODE_TYPE_COLOR.end;
          return (
            <div key={i} style={{ display: "flex", alignItems: "center", gap: 4 }}>
              <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: c.fg, background: c.bg, border: `1px solid ${c.fg}22`, borderRadius: 4, padding: "2px 7px" }}>{n.label}</span>
              {i < tpl.previewNodes.length - 1 && <span style={{ fontSize: 10, color: "var(--fg-dim)" }}>→</span>}
            </div>
          );
        })}
      </div>

      <button
        onClick={onUse}
        disabled={disabled}
        style={{
          height: 34,
          background: hovered && !disabled ? "var(--accent)" : "transparent",
          border: `1px solid ${hovered && !disabled ? "var(--accent)" : "var(--border-strong)"}`,
          borderRadius: "var(--r-2)",
          color: hovered && !disabled ? "var(--accent-fg)" : "var(--fg-muted)",
          fontSize: 12,
          fontWeight: 600,
          cursor: disabled ? "default" : "pointer",
          fontFamily: "var(--font-sans)",
          transition: "background 0.15s, border-color 0.15s, color 0.15s",
          opacity: disabled && !loading ? 0.5 : 1,
        }}
      >
        {loading ? "Creating…" : "Use template →"}
      </button>
    </div>
  );
}

function fmtDate(iso?: string): string {
  if (!iso) return "—";
  try {
    return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
  } catch {
    return iso;
  }
}

// Shared styles
const navBtnStyle: React.CSSProperties = {
  background: "transparent", border: "none", cursor: "pointer",
  fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 8px",
};

const ghostBtnSm: React.CSSProperties = {
  height: 28, padding: "0 10px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
const ghostBtn: React.CSSProperties = {
  height: 36, padding: "0 14px", fontSize: 13, fontWeight: 500,
  background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center",
};
const primaryBtn: React.CSSProperties = {
  height: 36, padding: "0 14px", fontSize: 13, fontWeight: 600,
  background: "var(--accent)", border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center",
};
