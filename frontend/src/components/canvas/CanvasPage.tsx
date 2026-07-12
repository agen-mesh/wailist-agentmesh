"use client";
import { useState, useMemo, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { WorkflowNode, WorkflowEdge, Workflow } from "@/lib/types";
import { Toast, Logo, Pill, Hairline, IconPlay, IconStop } from "@/components/ui";
import { workflows as workflowsApi } from "@/lib/api";
import { CanvasGraph } from "./CanvasGraph";
import { PalettePanel } from "./PalettePanel";
import { Inspector } from "./Inspector";
import { LogDrawer } from "./LogDrawer";

interface CanvasPageProps {
  workflowId: string;
}

export function CanvasPage({ workflowId }: CanvasPageProps) {
  const router = useRouter();

  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [logOpen, setLogOpen] = useState(false);
  const [deployed, setDeployed] = useState(false);
  const [running, setRunning] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [saveLabel, setSaveLabel] = useState("");
  const [runId, setRunId] = useState<string | null>(null);
  const [chatPrompt, setChatPrompt] = useState<string | null>(null); // null = closed
  const justLoaded = useRef(true);

  useEffect(() => {
    setLoading(true);
    setSelectedId(null);
    setDeployed(false);
    setRunning(false);

    if (workflowId === "new") {
      workflowsApi.create("Untitled workflow")
        .then((wf) => router.replace(`/workflows/${wf.id}`))
        .catch(() => setLoading(false));
      return;
    }

    workflowsApi.get(workflowId)
      .then((wf) => {
        justLoaded.current = true;
        setWorkflow(wf);
        // Restore deployed state: if any agent node has a wallet address it was previously deployed.
        if (wf.nodes.some((n) => n.type === "agent" && n.wallet)) {
          setDeployed(true);
        }
        setLoading(false);
      })
      .catch(() => { router.push("/workflows"); });
  }, [workflowId, router]);

  // Auto-save: debounce 1.5s after any change, skip on initial load.
  useEffect(() => {
    if (!workflow) return;
    if (justLoaded.current) { justLoaded.current = false; return; }
    setSaveLabel("saving…");
    const t = setTimeout(() => {
      workflowsApi.update(workflow.id, { name: workflow.name, nodes: workflow.nodes, edges: workflow.edges })
        .then(() => {
          const now = new Date();
          setSaveLabel(`saved · ${now.getHours()}:${String(now.getMinutes()).padStart(2, "0")}`);
        })
        .catch(() => setSaveLabel("save failed"));
    }, 1500);
    return () => clearTimeout(t);
  }, [workflow]);

  const selected = useMemo(
    () => workflow?.nodes.find((n) => n.id === selectedId) ?? null,
    [workflow, selectedId]
  );

  const attachedSummaries = useMemo(() => {
    const out: Record<string, { model: string | null; tools: number }> = {};
    if (!workflow) return out;
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
    setWorkflow((wf) => wf ? ({ ...wf, nodes: wf.nodes.map((x) => (x.id === n.id ? n : x)) }) : wf);
  }, []);

  const onDelete = useCallback(() => {
    if (!selectedId) return;
    setWorkflow((wf) => wf ? ({
      ...wf,
      nodes: wf.nodes.filter((n) => n.id !== selectedId),
      edges: wf.edges.filter((e) => e.from !== selectedId && e.to !== selectedId),
    }) : wf);
    setSelectedId(null);
  }, [selectedId]);

  // Delete/Backspace removes the selected node — ignored while typing in a field.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Delete" && e.key !== "Backspace") return;
      if (!selectedId) return;
      const el = document.activeElement as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
      e.preventDefault();
      onDelete();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedId, onDelete]);

  const onDeploy = useCallback(async () => {
    if (!workflow) return;
    if (deployed) { showToast("Re-deployed · wallets preserved"); return; }
    try {
      const res = await workflowsApi.deploy(workflow.id);
      setWorkflow((wf) => {
        if (!wf) return wf;
        const addrMap: Record<string, string> = {};
        for (const a of res.agents) addrMap[a.nodeId] = a.address;
        return {
          ...wf,
          nodes: wf.nodes.map((n) =>
            n.type === "agent" && addrMap[n.id]
              ? { ...n, wallet: addrMap[n.id], balance: "0.000000", spent: "0.000000" }
              : n
          ),
        };
      });
      setDeployed(true);
      showToast(`Deployed · ${res.agents.length} wallet${res.agents.length !== 1 ? "s" : ""} provisioned on Algorand testnet`);
    } catch (err: unknown) {
      showToast(`Deploy failed · ${err instanceof Error ? err.message : "unknown error"}`);
    }
  }, [deployed, workflow, showToast]);

  const hasChatTrigger = useMemo(
    () => workflow?.nodes.some((n) => n.type === "trigger" && n.template === "chat") ?? false,
    [workflow]
  );

  const startRun = useCallback(async (input?: Record<string, unknown>) => {
    if (!workflow) return;
    try {
      const res = await workflowsApi.run(workflow.id, input);
      setRunId(res.runId);
      setRunning(true);
      setLogOpen(true);
      setChatPrompt(null);
      showToast(`Run started · ${res.runId.slice(0, 8)}…`);
    } catch (err: unknown) {
      showToast(`Run failed · ${err instanceof Error ? err.message : "unknown error"}`);
    }
  }, [workflow, showToast]);

  const onRun = useCallback(async () => {
    if (!workflow) return;
    if (!deployed) { showToast("Deploy first to run"); return; }
    if (running) {
      try { await workflowsApi.stop(workflow.id); } catch { /* ignore */ }
      setRunning(false);
      return;
    }
    if (hasChatTrigger) {
      setChatPrompt(""); // open dialog
      return;
    }
    await startRun();
  }, [workflow, deployed, running, hasChatTrigger, startRun, showToast]);

  const totalSpend = (workflow?.nodes.filter((n) => n.type === "agent").reduce((s, n) => s + parseFloat(n.spent ?? "0"), 0) ?? 0).toFixed(3);

  const onDragNodeStart = useCallback((e: React.DragEvent, meta: Partial<WorkflowNode>) => {
    e.dataTransfer.setData("application/agentmesh", JSON.stringify(meta));
    e.dataTransfer.effectAllowed = "move";
  }, []);

  // Wrapper typed as non-null so child components don't need to change.
  // Safe because children only render after the null guard above.
  const setWorkflowNN = useCallback(
    (val: Workflow | ((prev: Workflow) => Workflow)) => {
      setWorkflow((wf) => {
        if (wf === null) return wf;
        return typeof val === "function" ? val(wf) : val;
      });
    },
    [setWorkflow]
  ) as React.Dispatch<React.SetStateAction<Workflow>>;

  if (loading || !workflow) {
    return (
      <div style={{ height: "100vh", display: "flex", alignItems: "center", justifyContent: "center", background: "var(--bg)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
        {workflowId === "new" ? "creating workflow…" : "loading…"}
      </div>
    );
  }

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", overflow: "hidden", background: "var(--bg)" }}>
      <CanvasTopbar
        workflow={workflow} setWorkflow={setWorkflowNN}
        deployed={deployed} running={running}
        onDeploy={onDeploy} onRun={onRun}
        totalSpend={totalSpend} saveLabel={saveLabel}
        onBack={() => router.push("/workflows")}
      />

      <div style={{ flex: 1, display: "flex", position: "relative", overflow: "hidden" }}>
        <PalettePanel onDragNodeStart={onDragNodeStart} />

        <div style={{ flex: 1, position: "relative", display: "flex", flexDirection: "column" }}>
          <CanvasGraph
            workflow={workflow} setWorkflow={setWorkflowNN}
            selectedId={selectedId} setSelectedId={setSelectedId}
            deployed={deployed} running={running}
            attachedSummaries={attachedSummaries}
          />
          <LogDrawer
            open={logOpen} onToggle={() => setLogOpen((o) => !o)}
            runId={runId} running={running}
            onRunComplete={() => setRunning(false)}
          />
        </div>

        <Inspector
          selected={selected} deployed={deployed} workflowId={workflow.id}
          onUpdate={onUpdate} onDelete={onDelete} onClose={() => setSelectedId(null)}
        />
      </div>

      {toast && <Toast message={toast} />}

      {chatPrompt !== null && (
        <ChatRunModal
          value={chatPrompt}
          onChange={setChatPrompt}
          onSubmit={(msg) => startRun({ message: msg })}
          onClose={() => setChatPrompt(null)}
        />
      )}
    </div>
  );
}

// ── Topbar ─────────────────────────────────────────────────────────────────
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, saveLabel, onBack }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; saveLabel: string;
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
        style={{ background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontWeight: 500, fontFamily: "var(--font-sans)", flex: "0 1 200px", minWidth: 0, padding: "4px 6px", borderRadius: 4 }}
      />
      <Pill mono dot tone={deployed ? "ok" : "default"}>{deployed ? "deployed · testnet" : "draft"}</Pill>
      {saveLabel && <Pill mono>{saveLabel}</Pill>}

      <div style={{ flex: 1 }} />

      <div style={{ display: "flex", alignItems: "center", gap: 14, padding: "0 14px", borderLeft: "1px solid var(--border)", borderRight: "1px solid var(--border)", height: 36, flexShrink: 0 }}>
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

// ── Chat Run Modal ──────────────────────────────────────────────────────────
function ChatRunModal({ value, onChange, onSubmit, onClose }: {
  value: string;
  onChange: (v: string) => void;
  onSubmit: (msg: string) => void;
  onClose: () => void;
}) {
  const submit = () => { if (value.trim()) onSubmit(value.trim()); };

  return (
    <div style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.7)", backdropFilter: "blur(4px)", zIndex: 100, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ width: 480, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: 12, padding: 24, display: "flex", flexDirection: "column", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>Start run</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 2 }}>chat trigger · type your message below</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 16, padding: 4 }}>✕</button>
        </div>
        <textarea
          autoFocus
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(); }}
          placeholder="e.g. What's the weather in Tokyo right now?"
          style={{ width: "100%", minHeight: 100, padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
        />
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>⌘ Enter to run</span>
          <div style={{ display: "flex", gap: 8 }}>
            <button onClick={onClose} style={{ ...ghostBtnSm, height: 32 }}>Cancel</button>
            <button onClick={submit} disabled={!value.trim()}
              style={{ ...primaryBtnStyle, height: 32, opacity: !value.trim() ? 0.5 : 1 }}>
              <IconPlay size={10} /> Run workflow
            </button>
          </div>
        </div>
      </div>
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
