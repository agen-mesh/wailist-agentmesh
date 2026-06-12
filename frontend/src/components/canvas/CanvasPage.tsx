"use client";
import { useState, useMemo, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { WorkflowNode, WorkflowEdge, Workflow } from "@/lib/types";
import { Toast, Logo, Pill, Hairline, IconPlay, IconStop } from "@/components/ui";
import { workflows as workflowsApi, auth as authApi } from "@/lib/api";
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
  const [spend, setSpend] = useState<{ total: number; last24h: number }>({ total: 0, last24h: 0 });
  const justLoaded = useRef(true);

  const refreshSpend = useCallback(() => {
    authApi.getSpend().then(setSpend).catch(() => {});
  }, []);

  useEffect(() => { refreshSpend(); }, [refreshSpend]);

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

  const onDeploy = useCallback(async () => {
    if (!workflow) return;
    if (deployed) { showToast("Re-deployed · wallets preserved"); return; }
    try {
      const res = await workflowsApi.deploy(workflow.id);
      setWorkflow((wf) => {
        if (!wf) return wf;
        const addrMap: Record<string, string> = {};
        for (const a of res.agents) addrMap[a.nodeId] = a.address;
        const webhookMap: Record<string, string> = {};
        for (const wh of (res.webhooks ?? [])) webhookMap[wh.nodeId] = wh.url;
        return {
          ...wf,
          nodes: wf.nodes.map((n) => {
            if (n.type === "agent" && addrMap[n.id]) return { ...n, wallet: addrMap[n.id], balance: "0.000000", spent: "0.000000" };
            if (n.type === "trigger" && webhookMap[n.id]) return { ...n, webhookLiveURL: webhookMap[n.id] };
            return n;
          }),
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

  const totalSpend = spend.total.toFixed(4);
  const spend24h = spend.last24h.toFixed(4);

  const estimatedCost = useMemo(() => {
    if (!workflow) return { usd: 0, algo: 0 };

    // USD cost per typical LLM call (~500 input + 200 output tokens at published rates)
    const USD_PER_CALL: Record<string, number> = {
      "gemini-2.5-flash":           0.0001,
      "gemini-2.5-pro":             0.0025,
      "gemini-2.0-flash":           0.00006,
      "gemini-1.5-pro":             0.00175,
      "gemini-1.5-flash":           0.0001,
      "gpt-4.1":                    0.0031,
      "gpt-4o":                     0.00375,
      "gpt-4o-mini":                0.00025,
      "claude-sonnet-4-6":          0.00465,
      "claude-opus-4-8":            0.0225,
      "claude-haiku-4-5":           0.0005,
      "claude-3-5-sonnet-20241022": 0.00465,
      "llama-3.3-70b-versatile":    0.0004,
      "llama-3.1-8b-instant":       0.00006,
      "mistral-large-latest":       0.0023,
      "mistral-medium-latest":      0.00065,
      "mistral-small-latest":       0.00015,
    };

    const nodeById = new Map(workflow.nodes.map(n => [n.id, n]));

    // Walk attach edges — check both node types rather than relying on edge direction,
    // since users can wire provider→agent or agent→provider and we store both.
    const agentModels = new Map<string, { model: string; useOurKey: boolean }>();
    const agentsWithTools = new Set<string>();
    for (const edge of workflow.edges) {
      if (edge.kind !== "attach") continue;
      const a = nodeById.get(edge.from);
      const b = nodeById.get(edge.to);
      if (!a || !b) continue;

      const provider = a.type === "provider" ? a : b.type === "provider" ? b : null;
      const agent    = a.type === "agent"    ? a : b.type === "agent"    ? b : null;
      const isTool   = a.type === "tool402" || a.type === "tool" || b.type === "tool402" || b.type === "tool";

      if (provider && agent && provider.model) {
        agentModels.set(agent.id, { model: provider.model, useOurKey: provider.useOurKey ?? true });
      }
      if (isTool && agent) {
        agentsWithTools.add(agent.id);
      }
    }

    let usd = 0;
    let algo = 0;
    for (const node of workflow.nodes) {
      if (node.type === "agent") {
        const info = agentModels.get(node.id);
        if (info) {
          const base = USD_PER_CALL[info.model] ?? 0.003;
          const cost = info.useOurKey ? base * 1.3 : base;
          // Agents with tools run an agentic loop (~3 LLM calls on avg per run)
          usd += cost * (agentsWithTools.has(node.id) ? 3 : 1);
        }
      }
      if (node.type === "tool402" && node.price) {
        algo += Math.abs(parseFloat(node.price) || 0);
      }
    }
    return { usd, algo };
  }, [workflow]);

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
        totalSpend={totalSpend} spend24h={spend24h} saveLabel={saveLabel}
        onBack={() => router.push("/workflows")}
        estimatedCost={estimatedCost}
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
            onRunComplete={() => { setRunning(false); refreshSpend(); }}
          />
        </div>

        <Inspector
          selected={selected} deployed={deployed} workflowId={workflow.id}
          onUpdate={onUpdate} onDelete={onDelete}
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
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, spend24h, saveLabel, onBack, estimatedCost }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; spend24h: string; saveLabel: string;
  onBack: () => void;
  estimatedCost: { usd: number; algo: number };
}) {
  return (
    <div style={{ height: 52, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 14px", gap: 10, overflow: "hidden" }}>
      {/* Left group — shrinks when viewport is narrow */}
      <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0, flexShrink: 1 }}>
        <button onClick={onBack} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex", flexShrink: 0 }}>
          <Logo size={16} />
        </button>
        <Hairline vertical length={20} />
        <button onClick={onBack} style={{ ...ghostBtnSm, flexShrink: 0 }}>← Workflows</button>
        <span style={{ color: "var(--fg-dim)", flexShrink: 0 }}>/</span>
        <input
          value={workflow.name}
          onChange={(e) => setWorkflow((wf) => ({ ...wf, name: e.target.value }))}
          style={{ background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontWeight: 500, fontFamily: "var(--font-sans)", width: 160, minWidth: 60, flexShrink: 1, padding: "4px 6px", borderRadius: 4 }}
        />
        <div style={{ flexShrink: 0 }}><Pill mono dot tone={deployed ? "ok" : "default"}>{deployed ? "deployed · testnet" : "draft"}</Pill></div>
        {saveLabel && <div style={{ flexShrink: 0 }}><Pill mono>{saveLabel}</Pill></div>}
      </div>

      <div style={{ flex: 1, minWidth: 8 }} />

      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "0 12px", borderLeft: "1px solid var(--border)", borderRight: "1px solid var(--border)", height: 36, flexShrink: 0 }}>
        <Stat label="agents" value={workflow.nodes.filter((n) => n.type === "agent").length} />
        <Stat label="tools"  value={workflow.nodes.filter((n) => n.type === "tool" || n.type === "tool402").length} />
        <Stat label="x402"   value={workflow.nodes.filter((n) => n.type === "tool402").length} color="#E879F9" />
        <Stat label="spent / 24h" value={`$${spend24h}`} color="var(--accent)" />
        <Stat label="total spent" value={`$${totalSpend}`} color="var(--fg-muted)" />
        {estimatedCost.usd > 0 && (
          <Stat label="est. llm" value={`~$${estimatedCost.usd.toFixed(4)}`} color={estimatedCost.usd > 0.05 ? "var(--warm)" : "var(--accent)"} />
        )}
        {estimatedCost.algo > 0 && (
          <Stat label="est. algo" value={`~${estimatedCost.algo.toFixed(4)} Ⓐ`} color="#E879F9" />
        )}
        {estimatedCost.usd === 0 && estimatedCost.algo === 0 && (
          <Stat label="est. / run" value="—" color="var(--fg-dim)" />
        )}
      </div>

      <button style={{ ...ghostBtnSm, flexShrink: 0 }}>Share</button>
      <button onClick={onDeploy} style={{ ...btnStyle, flexShrink: 0 }}>{deployed ? "Re-deploy" : "Deploy"}</button>
      <button onClick={onRun} disabled={!deployed} title={!deployed ? "Deploy first" : "Run workflow"}
        style={{ ...primaryBtnStyle, minWidth: 72, justifyContent: "center", opacity: !deployed ? 0.5 : 1, flexShrink: 0 }}>
        {running ? <><IconStop size={10} /> Stop</> : <><IconPlay size={12} /> Run</>}
      </button>
      <Hairline vertical length={20} />
      <div style={{ width: 28, height: 28, borderRadius: 999, background: "var(--accent)", color: "var(--accent-fg)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 11, fontWeight: 700, flexShrink: 0 }}>AC</div>
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
