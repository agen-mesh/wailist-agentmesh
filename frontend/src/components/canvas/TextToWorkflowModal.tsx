"use client";
import { useState, useEffect } from "react";
import { Workflow, WorkflowNode, WorkflowEdge, MarketplaceEndpoint } from "@/lib/types";
import { MARKETPLACE_ENDPOINTS } from "@/lib/data";
import { marketplace } from "@/lib/api";
import {
  buildDraftPlan,
  applyAnswers,
  buildWorkflow,
  DraftPlan,
  ClarifyQuestion,
} from "@/lib/textToWorkflow";

interface TextToWorkflowModalProps {
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  onClose: () => void;
}

type Stage = "input" | "questions" | "preview";

export function TextToWorkflowModal({ setWorkflow, onClose }: TextToWorkflowModalProps) {
  const [stage, setStage]         = useState<Stage>("input");
  const [text, setText]           = useState("");
  const [endpoints, setEndpoints] = useState<MarketplaceEndpoint[]>(MARKETPLACE_ENDPOINTS);
  const [draft, setDraft]         = useState<DraftPlan | null>(null);
  const [answers, setAnswers]     = useState<Record<string, string>>({});
  const [preview, setPreview]     = useState<{ nodes: WorkflowNode[]; edges: WorkflowEdge[] } | null>(null);

  // Fetch live GoPlausible endpoints on open; mounted flag prevents stale setState on unmount
  useEffect(() => {
    let mounted = true;
    marketplace.goplausibleList(50, 0)
      .then((r) => { if (mounted) setEndpoints([...MARKETPLACE_ENDPOINTS, ...r.endpoints]); })
      .catch(() => {});
    return () => { mounted = false; };
  }, []);

  const onGenerate = () => {
    if (!text.trim()) return;
    const d = buildDraftPlan(text.trim(), endpoints);
    setDraft(d);
    if (d.questions.length === 0) {
      // No ambiguity — go straight to preview
      const result = buildWorkflow(d);
      setPreview(result);
      setStage("preview");
    } else {
      const initial: Record<string, string> = {};
      for (const q of d.questions) initial[q.id] = "";
      setAnswers(initial);
      setStage("questions");
    }
  };

  const onSubmitAnswers = () => {
    if (!draft) return;
    const resolved = applyAnswers(draft, answers);
    const result   = buildWorkflow(resolved);
    setPreview(result);
    setStage("preview");
  };

  const onApply = () => {
    if (!preview || preview.nodes.length === 0) return;
    setWorkflow((wf) => ({
      ...wf,
      nodes: [...wf.nodes, ...preview.nodes],
      edges: [...wf.edges, ...preview.edges],
    }));
    onClose();
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.75)", backdropFilter: "blur(4px)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 520, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: 24, display: "flex", flexDirection: "column", gap: 16 }}>
        {/* Header */}
        <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>
              {stage === "input"     && "✨ Generate workflow"}
              {stage === "questions" && "A few quick questions"}
              {stage === "preview"   && "Workflow preview"}
            </div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 2 }}>
              {stage === "input"     && "describe what you want — no LLM required"}
              {stage === "questions" && `${draft?.questions.length ?? 0} of 3 max · resolves ambiguous slots`}
              {stage === "preview"   && `${preview?.nodes.length ?? 0} nodes · ${preview?.edges.length ?? 0} edges — will be appended to canvas`}
            </div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 16, padding: 4 }}>✕</button>
        </div>

        {/* ── Stage: input ── */}
        {stage === "input" && (
          <>
            <textarea
              autoFocus
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) onGenerate(); }}
              placeholder="e.g. I want a weather reporter that emails me every morning"
              style={{ width: "100%", minHeight: 90, padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
            />
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>⌘ Enter to generate</span>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={onClose} style={ghostBtn}>Cancel</button>
                <button onClick={onGenerate} disabled={!text.trim()} style={{ ...primaryBtn, opacity: !text.trim() ? 0.5 : 1 }}>
                  Generate →
                </button>
              </div>
            </div>
          </>
        )}

        {/* ── Stage: questions ── */}
        {stage === "questions" && draft && (
          <>
            <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
              {draft.questions.map((q) => (
                <QuestionBlock
                  key={q.id}
                  question={q}
                  value={answers[q.id] ?? ""}
                  onChange={(v) => setAnswers((a) => ({ ...a, [q.id]: v }))}
                />
              ))}
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <button onClick={() => setStage("input")} style={ghostBtn}>← Back</button>
              <button onClick={onSubmitAnswers} style={primaryBtn}>Preview →</button>
            </div>
          </>
        )}

        {/* ── Stage: preview ── */}
        {stage === "preview" && preview && draft && (
          <>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {preview.nodes.map((n) => (
                <NodePreviewRow key={n.id} node={n} />
              ))}
            </div>
            <div style={{ padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginBottom: 4 }}>workflow name</div>
              <div style={{ fontSize: 13, fontWeight: 500, color: "var(--fg)" }}>{draft.name}</div>
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <button onClick={() => { setDraft(null); setAnswers({}); setPreview(null); setStage("input"); }} style={ghostBtn}>Start over</button>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={onClose} style={ghostBtn}>Cancel</button>
                <button onClick={onApply} style={primaryBtn}>Apply to canvas</button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ── QuestionBlock ────────────────────────────────────────────────────────────

function QuestionBlock({ question, value, onChange }: {
  question: ClarifyQuestion;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div style={{ fontSize: 13, fontWeight: 500, color: "var(--fg)" }}>{question.prompt}</div>
      {question.options && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {question.options.map((opt) => (
            <button
              key={opt.value}
              onClick={() => onChange(opt.value)}
              style={{
                textAlign: "left", padding: "9px 12px",
                background: value === opt.value ? "var(--accent-soft)" : "var(--bg)",
                border: `1px solid ${value === opt.value ? "var(--accent-line)" : "var(--border)"}`,
                borderRadius: "var(--r-2)", color: value === opt.value ? "var(--accent)" : "var(--fg)",
                fontSize: 12, cursor: "pointer", fontFamily: "var(--font-sans)", transition: "all 0.1s",
              }}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
      {question.freeText && (
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="e.g. you@example.com"
          style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }}
        />
      )}
    </div>
  );
}

// ── NodePreviewRow ───────────────────────────────────────────────────────────

const NODE_TYPE_COLORS: Record<string, string> = {
  trigger:  "var(--fg-muted)",
  agent:    "var(--accent)",
  provider: "var(--accent)",
  tool:     "var(--fg-muted)",
  tool402:  "#E879F9",
  action:   "var(--fg-muted)",
  end:      "var(--fg-dim)",
};

function NodePreviewRow({ node }: { node: WorkflowNode }) {
  const color = NODE_TYPE_COLORS[node.type] ?? "var(--fg)";
  const label = node.name ?? node.label ?? node.type;
  const sub   = node.model
    ? node.model
    : node.price
    ? `x402 · $${node.price}/${node.unit}`
    : node.template ?? node.type;

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
      <span style={{ width: 22, height: 22, borderRadius: 6, background: node.type === "tool402" ? "rgba(232,121,249,0.12)" : "var(--bg-elev-2)", color, display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 12, flexShrink: 0 }}>
        {node.icon ?? "·"}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)" }}>{label}</div>
        <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)", textOverflow: "ellipsis", overflow: "hidden", whiteSpace: "nowrap" }}>{sub}</div>
      </div>
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color, flexShrink: 0 }}>{node.type}</span>
    </div>
  );
}

// ── Shared button styles ────────────────────────────────────────────────────

const ghostBtn: React.CSSProperties = {
  height: 32, padding: "0 12px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};

const primaryBtn: React.CSSProperties = {
  height: 32, padding: "0 14px", fontSize: 12, fontWeight: 600,
  background: "var(--accent)", border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
