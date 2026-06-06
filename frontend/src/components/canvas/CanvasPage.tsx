"use client";
import { useState, useMemo, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { WorkflowNode, WorkflowEdge, Workflow } from "@/lib/types";
import { SAMPLE_WORKFLOW } from "@/lib/data";
import { Toast, Logo, Pill, Hairline, IconPlay, IconStop } from "@/components/ui";
import { CanvasGraph } from "./CanvasGraph";
import { PalettePanel } from "./PalettePanel";
import { Inspector } from "./Inspector";
import { LogDrawer } from "./LogDrawer";

interface CanvasPageProps {
  workflowId: string;
}

export function CanvasPage({ workflowId }: CanvasPageProps) {
  const router = useRouter();

  const initialWorkflow = useMemo<Workflow>(() => {
    if (workflowId === "new") return { id: "wf-new", name: "Untitled workflow", nodes: [], edges: [] };
    return JSON.parse(JSON.stringify(SAMPLE_WORKFLOW));
  }, [workflowId]);

  const [workflow, setWorkflow] = useState<Workflow>(initialWorkflow);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [logOpen, setLogOpen] = useState(false);
  const [deployed, setDeployed] = useState(false);
  const [running, setRunning] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => { setWorkflow(initialWorkflow); setDeployed(false); setRunning(false); setSelectedId(null); }, [initialWorkflow]);

  const selected = useMemo(
    () => workflow.nodes.find((n) => n.id === selectedId) ?? null,
    [workflow, selectedId]
  );

  const attachedSummaries = useMemo(() => {
    const out: Record<string, { model: string | null; tools: number }> = {};
    for (const n of workflow.nodes) {
      if (n.type !== "agent") continue;
      let modelName: string | null = null;
      let toolsCount = 0;
      for (const e of workflow.edges) {
        if (e.kind !== "attach" || e.to !== n.id) continue;
        const src = workflow.nodes.find((x) => x.id === e.from);
        if (!src) continue;
        if (e.toPort === "model" && src.type === "provider") modelName = src.name ?? null;
        if (e.toPort === "tools" && (src.type === "tool" || src.type === "tool402")) toolsCount++;
      }
      out[n.id] = { model: modelName, tools: toolsCount };
    }
    return out;
  }, [workflow]);

  const showToast = useCallback((msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(null), 2400);
  }, []);

  const onUpdate = useCallback((n: WorkflowNode) => {
    setWorkflow((wf) => ({ ...wf, nodes: wf.nodes.map((x) => (x.id === n.id ? n : x)) }));
  }, []);

  const onDelete = useCallback(() => {
    if (!selectedId) return;
    setWorkflow((wf) => ({
      ...wf,
      nodes: wf.nodes.filter((n) => n.id !== selectedId),
      edges: wf.edges.filter((e) => e.from !== selectedId && e.to !== selectedId),
    }));
    setSelectedId(null);
  }, [selectedId]);

  const onFund = useCallback(() => {
    if (!selected) return;
    const newBal = (parseFloat(selected.balance ?? "0") + 5).toFixed(3);
    onUpdate({ ...selected, balance: newBal });
    showToast(`Funded ${selected.name} with 5 ALGO · tx 0x${Math.random().toString(16).slice(2, 6)}…`);
  }, [selected, onUpdate, showToast]);

  const onDeploy = useCallback(() => {
    if (deployed) { showToast("Re-deployed · wallets preserved"); return; }
    const newNodes = workflow.nodes.map((n) => {
      if (n.type !== "agent") return n;
      const suffix = Math.random().toString(36).slice(2, 6).toUpperCase();
      return { ...n, wallet: `ALGO${Math.random().toString(36).slice(2, 6).toUpperCase()}…${suffix}`, balance: "0.000", spent: "0.000" };
    });
    setWorkflow((wf) => ({ ...wf, nodes: newNodes }));
    setDeployed(true);
    const count = workflow.nodes.filter((n) => n.type === "agent").length;
    showToast(`Deployed · ${count} wallet${count !== 1 ? "s" : ""} provisioned on Algorand testnet`);
  }, [deployed, workflow.nodes, showToast]);

  const onRun = useCallback(() => {
    if (!deployed) { showToast("Deploy first to run"); return; }
    setRunning((r) => {
      if (!r) { setLogOpen(true); showToast(`Run started · r-${Math.floor(1800 + Math.random() * 200)}`); }
      return !r;
    });
  }, [deployed, showToast]);

  const totalSpend = workflow.nodes.filter((n) => n.type === "agent").reduce((s, n) => s + parseFloat(n.spent ?? "0"), 0).toFixed(3);

  const onDragNodeStart = useCallback((e: React.DragEvent, meta: Partial<WorkflowNode>) => {
    e.dataTransfer.setData("application/agentmesh", JSON.stringify(meta));
    e.dataTransfer.effectAllowed = "move";
  }, []);

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", overflow: "hidden", background: "var(--bg)" }}>
      <CanvasTopbar
        workflow={workflow} setWorkflow={setWorkflow}
        deployed={deployed} running={running}
        onDeploy={onDeploy} onRun={onRun}
        totalSpend={totalSpend}
        onBack={() => router.push("/workflows")}
      />

      <div style={{ flex: 1, display: "flex", position: "relative", overflow: "hidden" }}>
        <PalettePanel onDragNodeStart={onDragNodeStart} />

        <div style={{ flex: 1, position: "relative", display: "flex", flexDirection: "column" }}>
          <CanvasGraph
            workflow={workflow} setWorkflow={setWorkflow}
            selectedId={selectedId} setSelectedId={setSelectedId}
            deployed={deployed} running={running}
            attachedSummaries={attachedSummaries}
          />
          <LogDrawer open={logOpen} onToggle={() => setLogOpen((o) => !o)} />
        </div>

        <Inspector
          selected={selected} deployed={deployed}
          onUpdate={onUpdate} onDelete={onDelete} onFund={onFund}
        />
      </div>

      {toast && <Toast message={toast} />}
    </div>
  );
}

// ── Topbar ─────────────────────────────────────────────────────────────────
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, onBack }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string;
  onBack: () => void;
}) {
  return (
    <div style={{ height: 52, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 14px", gap: 14 }}>
      <button onClick={onBack} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex" }}>
        <Logo size={16} />
      </button>
      <Hairline vertical length={20} />
      <button onClick={onBack} style={ghostBtnSm}>← Workflows</button>
      <span style={{ color: "var(--fg-dim)" }}>/</span>
      <input
        value={workflow.name}
        onChange={(e) => setWorkflow((wf) => ({ ...wf, name: e.target.value }))}
        style={{ background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontWeight: 500, fontFamily: "var(--font-sans)", minWidth: 200, padding: "4px 6px", borderRadius: 4 }}
      />
      <Pill mono dot tone={deployed ? "ok" : "default"}>{deployed ? "deployed · testnet" : "draft"}</Pill>
      <Pill mono>auto-saved · 12s ago</Pill>

      <div style={{ flex: 1 }} />

      <div style={{ display: "flex", alignItems: "center", gap: 14, padding: "0 14px", borderLeft: "1px solid var(--border)", borderRight: "1px solid var(--border)", height: 36 }}>
        <Stat label="agents" value={workflow.nodes.filter((n) => n.type === "agent").length} />
        <Stat label="tools"  value={workflow.nodes.filter((n) => n.type === "tool" || n.type === "tool402").length} />
        <Stat label="x402"   value={workflow.nodes.filter((n) => n.type === "tool402").length} color="#E879F9" />
        <Stat label="spent / 24h" value={totalSpend} unit="ALGO" color="var(--accent)" />
      </div>

      <button style={ghostBtnSm}>Share</button>
      <button onClick={onDeploy} style={btnStyle}>{deployed ? "Re-deploy" : "Deploy"}</button>
      <button onClick={onRun} disabled={!deployed} title={!deployed ? "Deploy first" : "Run workflow"}
        style={{ ...primaryBtnStyle, minWidth: 86, justifyContent: "center", opacity: !deployed ? 0.5 : 1 }}>
        {running ? <><IconStop size={10} /> Stop</> : <><IconPlay size={12} /> Run</>}
      </button>
      <Hairline vertical length={20} />
      <div style={{ width: 28, height: 28, borderRadius: 999, background: "var(--accent)", color: "var(--accent-fg)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 700 }}>AC</div>
    </div>
  );
}

function Stat({ label, value, unit, color }: { label: string; value: string | number; unit?: string; color?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>{label}</span>
      <span style={{ fontFamily: "var(--font-sans)", fontSize: 13, fontWeight: 500, color: color ?? "var(--fg)" }}>
        {value}{unit && <span style={{ color: "var(--fg-dim)", fontSize: 10, marginLeft: 3 }}>{unit}</span>}
      </span>
    </div>
  );
}

const ghostBtnSm: React.CSSProperties = {
  height: 28, padding: "0 10px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
const btnStyle: React.CSSProperties = {
  height: 28, padding: "0 12px", fontSize: 12, fontWeight: 500,
  background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
const primaryBtnStyle: React.CSSProperties = {
  height: 28, padding: "0 12px", fontSize: 12, fontWeight: 600,
  background: "var(--accent)", border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
