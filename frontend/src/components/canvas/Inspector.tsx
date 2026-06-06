"use client";
import { useState } from "react";
import { WorkflowNode } from "@/lib/types";
import { PROVIDER_TEMPLATES, TOOL_TEMPLATES, TOOL402_TEMPLATES, TRIGGER_TEMPLATES, ACTION_TEMPLATES, END_TEMPLATES, AGENT_TEMPLATES } from "@/lib/data";
import { Pill, IconClose, IconWallet } from "@/components/ui";

interface InspectorProps {
  selected: WorkflowNode | null;
  deployed: boolean;
  onUpdate: (n: WorkflowNode) => void;
  onDelete: () => void;
  onFund: () => void;
}

export function Inspector({ selected, deployed, onUpdate, onDelete, onFund }: InspectorProps) {
  if (!selected) return <EmptyInspector />;

  const meta = nodeMeta(selected);

  return (
    <div style={{ width: 320, flexShrink: 0, borderLeft: "1px solid var(--border)", background: "var(--bg-elev-1)", overflow: "auto", height: "100%" }}>
      {/* Header */}
      <div style={{ padding: "14px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span style={{ width: 24, height: 24, borderRadius: 6, background: meta.bg, color: meta.fg, border: "1px solid var(--border-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 12 }}>{meta.icon}</span>
          <div>
            <div style={{ fontSize: 13, fontWeight: 500 }}>{meta.title}</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>{selected.type} · {selected.id}</div>
          </div>
        </div>
        <button onClick={onDelete} style={{ width: 32, height: 32, display: "flex", alignItems: "center", justifyContent: "center", background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", borderRadius: "var(--r-2)" }}>
          <IconClose size={12} />
        </button>
      </div>

      <div style={{ padding: 16, display: "flex", flexDirection: "column", gap: 18 }}>
        {selected.type === "agent"    && <AgentInspector    node={selected} deployed={deployed} onUpdate={onUpdate} onFund={onFund} />}
        {selected.type === "provider" && <ProviderInspector node={selected} onUpdate={onUpdate} />}
        {selected.type === "tool"     && <ToolInspector     node={selected} onUpdate={onUpdate} />}
        {selected.type === "tool402"  && <Tool402Inspector  node={selected} onUpdate={onUpdate} />}
        {selected.type === "trigger"  && <TriggerInspector  node={selected} onUpdate={onUpdate} />}
        {selected.type === "action"   && <ActionInspector   node={selected} onUpdate={onUpdate} />}
        {selected.type === "end"      && <EndInspector      node={selected} onUpdate={onUpdate} />}
      </div>
    </div>
  );
}

function EmptyInspector() {
  return (
    <div style={{ width: 320, flexShrink: 0, borderLeft: "1px solid var(--border)", background: "var(--bg-elev-1)", padding: 20, display: "flex", flexDirection: "column" }}>
      <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 14 }}>inspector</div>
      <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", color: "var(--fg-dim)", textAlign: "center", padding: 24, fontSize: 12, lineHeight: 1.6 }}>
        <div style={{ width: 40, height: 40, borderRadius: 999, border: "1px dashed var(--border-strong)", display: "inline-flex", alignItems: "center", justifyContent: "center", marginBottom: 12 }}>◇</div>
        select a node to inspect<br />its config + connections.
      </div>
    </div>
  );
}

function nodeMeta(n: WorkflowNode) {
  const tpls: Record<string, { list: { id: string; icon?: string; name?: string }[]; bg: string; fg: string }> = {
    trigger:  { list: TRIGGER_TEMPLATES,  bg: "var(--bg-elev-3)",            fg: "var(--fg)" },
    agent:    { list: AGENT_TEMPLATES,    bg: "var(--accent-soft)",          fg: "var(--accent)" },
    provider: { list: PROVIDER_TEMPLATES, bg: "var(--bg-elev-3)",            fg: "var(--accent)" },
    tool:     { list: TOOL_TEMPLATES,     bg: "var(--bg-elev-3)",            fg: "var(--fg)" },
    tool402:  { list: TOOL402_TEMPLATES,  bg: "rgba(232, 121, 249, 0.14)",   fg: "#E879F9" },
    action:   { list: ACTION_TEMPLATES,   bg: "var(--bg-elev-3)",            fg: "var(--fg)" },
    end:      { list: END_TEMPLATES,      bg: "var(--bg-elev-3)",            fg: "var(--fg)" },
  };
  const L = tpls[n.type] ?? tpls.action;
  const tpl = L.list.find((x) => x.id === n.template);
  return { icon: n.icon ?? tpl?.icon ?? "◇", title: n.name ?? n.label ?? tpl?.name ?? "Custom node", bg: L.bg, fg: L.fg };
}

// ── Shared ─────────────────────────────────────────────────────────────────
function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 10 }}>{label}</div>
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>{children}</div>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 5 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", fontSize: 11, color: "var(--fg-muted)" }}>
        <span>{label}</span>
        {hint && <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>{hint}</span>}
      </div>
      {children}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  height: 36, padding: "0 10px", width: "100%",
  background: "var(--bg)", border: "1px solid var(--border)",
  borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 12,
  fontFamily: "var(--font-sans)", outline: "none",
};

const monoInputStyle: React.CSSProperties = { ...inputStyle, fontFamily: "var(--font-mono)", fontSize: 11 };

// ── Agent Inspector ────────────────────────────────────────────────────────
function AgentInspector({ node, deployed, onUpdate, onFund }: { node: WorkflowNode; deployed: boolean; onUpdate: (n: WorkflowNode) => void; onFund: () => void }) {
  return (
    <>
      <Section label="Identity">
        <Field label="Name">
          <input style={inputStyle} value={node.name ?? ""} onChange={(e) => onUpdate({ ...node, name: e.target.value })} />
        </Field>
      </Section>

      <Section label="Wallet">
        {deployed && node.wallet ? (
          <div style={{ padding: 14, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
            <div style={{ display: "flex", justifyContent: "space-between" }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>{node.wallet}</span>
              <Pill mono dot tone="ok">testnet</Pill>
            </div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginTop: 12 }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 26, fontWeight: 500, color: "var(--accent)" }}>{node.balance ?? "0.000"}</span>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>ALGO</span>
            </div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 4 }}>spent {node.spent ?? "0.000"} ALGO · last 24h</div>
            <div style={{ display: "flex", gap: 6, marginTop: 14 }}>
              <button onClick={onFund} style={{ flex: 1, height: 32, display: "flex", alignItems: "center", justifyContent: "center", gap: 6, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 12, fontWeight: 500, cursor: "pointer", fontFamily: "var(--font-sans)" }}>
                <IconWallet size={12} /> Fund · 5 ALGO
              </button>
              <button style={{ height: 32, padding: "0 10px", background: "transparent", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", color: "var(--fg-muted)", fontSize: 12, cursor: "pointer", fontFamily: "var(--font-sans)" }}>View ↗</button>
            </div>
          </div>
        ) : (
          <div style={{ padding: 14, background: "var(--bg)", border: "1px dashed var(--border-strong)", borderRadius: "var(--r-2)", fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.55 }}>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 8 }}>not yet deployed</div>
            This agent will receive an Ed25519 keypair on Algorand testnet when you click <strong style={{ color: "var(--fg)" }}>Deploy</strong>. You'll fund it manually from this panel after deploy.
          </div>
        )}
      </Section>

      <Section label="System prompt">
        <textarea style={{ ...inputStyle, height: "auto", padding: 10, resize: "vertical", lineHeight: 1.5 }} rows={5} value={node.systemPrompt ?? ""} onChange={(e) => onUpdate({ ...node, systemPrompt: e.target.value })} />
      </Section>

      <Section label="Limits">
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <Field label="Max spend / run"><input style={monoInputStyle} defaultValue="0.50 ALGO" /></Field>
          <Field label="Timeout"><input style={monoInputStyle} defaultValue="30s" /></Field>
        </div>
      </Section>
    </>
  );
}

// ── Provider Inspector ─────────────────────────────────────────────────────
function ProviderInspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = PROVIDER_TEMPLATES.find((t) => t.id === node.template);
  return (
    <>
      <Section label="Model">
        <Field label="Provider">
          {node.custom
            ? <input style={inputStyle} value={node.name ?? ""} placeholder="e.g. Together AI" onChange={(e) => onUpdate({ ...node, name: e.target.value })} />
            : <input style={inputStyle} value={tpl?.name ?? ""} readOnly />}
        </Field>
        <Field label="Model">
          {node.custom
            ? <input style={monoInputStyle} value={node.model ?? ""} placeholder="e.g. llama-3.3-70b" onChange={(e) => onUpdate({ ...node, model: e.target.value })} />
            : <select style={inputStyle} defaultValue={tpl?.model}>
                <option>{tpl?.model}</option>
                <option>gemini-1.5-flash</option>
                <option>gemini-1.5-pro</option>
              </select>}
        </Field>
      </Section>
      <Section label="Credentials">
        <Field label="API Key" hint="encrypted at rest">
          <input style={monoInputStyle} type="password" value={node.apiKey ?? ""} placeholder="AIza···" onChange={(e) => onUpdate({ ...node, apiKey: e.target.value })} />
        </Field>
      </Section>
      <Section label="Parameters">
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <Field label="Temperature"><input style={monoInputStyle} defaultValue="0.4" /></Field>
          <Field label="Max tokens"><input style={monoInputStyle} defaultValue="2048" /></Field>
        </div>
      </Section>
    </>
  );
}

// ── Tool Inspector ─────────────────────────────────────────────────────────
function ToolInspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = TOOL_TEMPLATES.find((t) => t.id === node.template);
  return (
    <>
      <Section label="Tool">
        {node.custom
          ? <Field label="Name"><input style={inputStyle} value={node.name ?? ""} placeholder="My HTTP tool" onChange={(e) => onUpdate({ ...node, name: e.target.value })} /></Field>
          : <><Field label="Type"><input style={inputStyle} value={tpl?.name ?? ""} readOnly /></Field>
             <Field label="Description"><input style={inputStyle} value={tpl?.desc ?? ""} readOnly /></Field></>}
      </Section>
      <Section label="Config">
        <Field label="Method">
          <select style={monoInputStyle} value={node.method ?? "GET"} onChange={(e) => onUpdate({ ...node, method: e.target.value })}>
            <option>GET</option><option>POST</option><option>PUT</option><option>DELETE</option>
          </select>
        </Field>
        <Field label="URL"><input style={monoInputStyle} value={node.url ?? ""} placeholder="https://api.example.com/v1/" onChange={(e) => onUpdate({ ...node, url: e.target.value })} /></Field>
      </Section>
    </>
  );
}

// ── Tool402 Inspector ──────────────────────────────────────────────────────
function Tool402Inspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = TOOL402_TEMPLATES.find((t) => t.id === node.template);
  const [draft, setDraft] = useState(node.endpoint ?? "");
  const [probing, setProbing] = useState(false);
  const magenta = "#E879F9";

  const discover = async () => {
    if (!draft.trim()) return;
    setProbing(true);
    onUpdate({ ...node, endpoint: draft.trim() });
    await new Promise((r) => setTimeout(r, 800));
    let host = draft;
    try { host = new URL(draft).host; } catch {}
    onUpdate({ ...node, endpoint: draft.trim(), price: "0.002", unit: "call", provider: host, priceLive: false });
    setProbing(false);
  };

  if (!node.custom) {
    return (
      <>
        <Section label="x402 endpoint">
          <div style={{ padding: 14, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", fontFamily: "var(--font-mono)", fontSize: 11 }}>
            <div style={{ color: "var(--fg-muted)" }}>{`https://${tpl?.provider}`}</div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginTop: 12 }}>
              <span style={{ color: magenta, fontSize: 22, fontWeight: 500 }}>{tpl?.price}</span>
              <span style={{ color: "var(--fg-muted)" }}>ALGO / {tpl?.unit}</span>
            </div>
          </div>
        </Section>
        <Section label="Settlement">
          <Field label="Payer"><input style={monoInputStyle} value="parent agent wallet" readOnly /></Field>
          <Field label="Max per call"><input style={monoInputStyle} defaultValue={`${tpl?.price} ALGO`} /></Field>
        </Section>
      </>
    );
  }

  return (
    <>
      <Section label="Identity">
        <Field label="Name"><input style={inputStyle} value={node.name ?? ""} onChange={(e) => onUpdate({ ...node, name: e.target.value })} /></Field>
      </Section>
      <Section label="x402 endpoint">
        <Field label="Endpoint URL" hint="HTTP 402 compliant">
          <input style={monoInputStyle} placeholder="https://api.your-service.x402/v1/search"
            value={draft} onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") discover(); }}
          />
        </Field>
        <button onClick={discover} disabled={!draft.trim() || probing}
          style={{ height: 32, display: "flex", alignItems: "center", justifyContent: "center", gap: 6, width: "100%", border: `1px solid ${magenta}`, background: "transparent", color: probing ? "var(--fg-dim)" : magenta, borderRadius: "var(--r-2)", fontSize: 12, cursor: "pointer", fontFamily: "var(--font-sans)", fontWeight: 500 }}>
          {probing ? <><span className="pulse">●</span> Probing endpoint…</> : (node.price ? "Re-test & refresh price" : "Test endpoint & fetch price")}
        </button>
        {node.price && !probing && (
          <div style={{ padding: 14, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", fontFamily: "var(--font-mono)", fontSize: 11 }}>
            <div style={{ color: "var(--fg-muted)" }}>{node.provider}</div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginTop: 12 }}>
              <span style={{ color: magenta, fontSize: 22, fontWeight: 500 }}>{node.price}</span>
              <span style={{ color: "var(--fg-muted)" }}>ALGO / {node.unit}</span>
            </div>
            <div style={{ marginTop: 8, color: node.priceLive ? "var(--accent)" : "var(--fg-dim)" }}>
              {node.priceLive ? "● live · read from HTTP 402 response" : "simulated · endpoint not reachable from browser"}
            </div>
          </div>
        )}
      </Section>
    </>
  );
}

// ── Trigger Inspector ──────────────────────────────────────────────────────
function TriggerInspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = TRIGGER_TEMPLATES.find((t) => t.id === node.template);
  return (
    <Section label="Trigger">
      {node.custom
        ? <Field label="Label"><input style={inputStyle} value={node.label ?? ""} placeholder="When …" onChange={(e) => onUpdate({ ...node, label: e.target.value })} /></Field>
        : <Field label="Type"><input style={inputStyle} value={tpl?.name ?? ""} readOnly /></Field>}
      {node.template === "cron"    && <Field label="Cron"><input style={monoInputStyle} defaultValue="0 9 * * *" /></Field>}
      {node.template === "webhook" && <Field label="Path"><input style={monoInputStyle} defaultValue="/in/abc123" /></Field>}
      {node.template === "chat"    && <Field label="Source"><input style={inputStyle} defaultValue="In-app chat widget" /></Field>}
    </Section>
  );
}

// ── Action Inspector ───────────────────────────────────────────────────────
function ActionInspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  return (
    <Section label="Action">
      <Field label="Name"><input style={inputStyle} value={node.name ?? ""} onChange={(e) => onUpdate({ ...node, name: e.target.value })} /></Field>
      {node.template === "email" && <>
        <Field label="To"><input style={monoInputStyle} defaultValue="{{ trigger.email }}" /></Field>
        <Field label="Subject"><input style={inputStyle} defaultValue="Your AgentMesh result" /></Field>
      </>}
      {node.template === "slack" && <Field label="Channel"><input style={monoInputStyle} defaultValue="#agent-output" /></Field>}
    </Section>
  );
}

// ── End Inspector ──────────────────────────────────────────────────────────
function EndInspector({ node, onUpdate }: { node: WorkflowNode; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = END_TEMPLATES.find((t) => t.id === node.template);
  return (
    <Section label="End">
      {node.custom
        ? <Field label="Label"><input style={inputStyle} value={node.label ?? ""} placeholder="Mark complete" onChange={(e) => onUpdate({ ...node, label: e.target.value })} /></Field>
        : <Field label="Type"><input style={inputStyle} value={tpl?.name ?? ""} readOnly /></Field>}
      <Field label="Status code"><input style={monoInputStyle} defaultValue="200" /></Field>
    </Section>
  );
}
