"use client";
import { useState, useMemo, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { Logo, Pill, Tag, Hairline, IconSearch, IconGrid } from "@/components/ui";
import { Workflow } from "@/lib/types";
import { useAuth } from "@/hooks/useAuth";
import { workflows as workflowsApi } from "@/lib/api";

export function WorkflowsPage() {
  const router = useRouter();
  const { signOut } = useAuth();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("all");
  const [view, setView] = useState<"rows" | "grid">("rows");
  const [wfList, setWfList] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

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

  const handleSignOut = async () => {
    await signOut();
    router.push("/");
  };

  const activeCount = wfList.filter((w) => w.status === "active").length;

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", overflow: "hidden", background: "var(--bg)" }}>
      {/* Topbar */}
      <div style={{ height: 56, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", padding: "0 24px", display: "flex", alignItems: "center", gap: 14 }}>
        <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0 }}>
          <Logo size={18} />
        </button>
        <Hairline vertical length={22} />
        <button style={ghostBtnSm}>Acme Capital ▾</button>
        <Pill mono dot tone="ok">testnet</Pill>
        <div style={{ flex: 1 }} />
        <button style={ghostBtnSm} onClick={() => router.push("/workflows")}>Workflow</button>
        <div className="profile-menu">
          <button className="profile-menu__trigger" aria-haspopup="menu" aria-label="Account menu">AC</button>
          <div className="profile-menu__panel" role="menu">
            <div className="profile-menu__card">
              <div style={{ padding: "12px 14px", display: "flex", alignItems: "center", gap: 10 }}>
                <div style={{ width: 30, height: 30, borderRadius: 999, background: "var(--accent)", color: "var(--accent-fg)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 700, flexShrink: 0 }}>AC</div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 13, fontWeight: 600, color: "var(--fg)" }}>Profile</div>
                  <div style={{ fontSize: 11, color: "var(--fg-dim)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>testnet workspace</div>
                </div>
              </div>
              <div className="profile-menu__divider" />
              <button className="profile-menu__item" role="menuitem">Usage</button>
              <button className="profile-menu__item" role="menuitem" onClick={() => router.push("/workflows")}>Workflow</button>
              <button className="profile-menu__item" role="menuitem">Settings</button>
              <div className="profile-menu__divider" />
              <button className="profile-menu__item profile-menu__item--danger" role="menuitem" onClick={handleSignOut}>Sign out</button>
            </div>
          </div>
        </div>
      </div>

      {/* Main */}
      <div style={{ flex: 1, overflow: "auto", background: "var(--bg)" }}>
        <div style={{ maxWidth: 1280, margin: "0 auto", padding: "36px 24px 80px" }}>
          {/* Header */}
          <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", marginBottom: 28 }}>
            <div>
              <Tag>your workspace</Tag>
              <h1 style={{ margin: "12px 0 4px", fontSize: 36, fontWeight: 500, letterSpacing: "-0.025em" }}>Workflows</h1>
              <p style={{ margin: 0, color: "var(--fg-muted)", fontSize: 14 }}>Design, deploy, and monitor agent pipelines.</p>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              <button style={ghostBtn}>Import</button>
              <button onClick={handleNewWorkflow} disabled={creating} style={{ ...primaryBtn, opacity: creating ? 0.6 : 1 }}>
                {creating ? "Creating…" : "+ New workflow"}
              </button>
            </div>
          </div>

          {/* KPI row */}
          <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 16, marginBottom: 24 }}>
            <KpiCard label="Active workflows" value={loading ? "…" : activeCount} sub={loading ? "" : `of ${wfList.length} total`} />
            <KpiCard label="Agents deployed" value="—" sub="deploy a workflow" />
            <KpiCard label="Spend · 30d" value="—" unit="ALGO" sub="run a workflow" />
            <KpiCard label="Runs · 30d" value="—" sub="no runs yet" tone="ok" />
          </div>

          {/* Controls */}
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
            <div style={{ position: "relative", flex: 1, maxWidth: 360 }}>
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
          ) : view === "rows" ? (
            <WorkflowRows items={filtered} onOpen={(id) => router.push(`/workflows/${id}`)} />
          ) : (
            <WorkflowGrid items={filtered} onOpen={(id) => router.push(`/workflows/${id}`)} />
          )}

          {!loading && filtered.length === 0 && (
            <div style={{ padding: 48, textAlign: "center", border: "1px dashed var(--border)", borderRadius: "var(--r-3)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              {wfList.length === 0 ? "no workflows yet — create one to get started" : "no workflows match"}
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

function WorkflowRows({ items, onOpen }: { items: Workflow[]; onOpen: (id: string) => void }) {
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
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 4 }}>
            <button style={ghostBtnSm} onClick={(e) => { e.stopPropagation(); onOpen(wf.id); }}>Open</button>
            <button style={{ ...ghostBtnSm, width: 28, padding: 0, justifyContent: "center" }} onClick={(e) => e.stopPropagation()}>⋯</button>
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

function fmtDate(iso?: string): string {
  if (!iso) return "—";
  try {
    return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
  } catch {
    return iso;
  }
}

// Shared styles
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
