"use client";
import { useState, useCallback } from "react";
import { WorkflowNode } from "@/lib/types";
import { PROVIDER_TEMPLATES, TOOL_TEMPLATES, TOOL402_TEMPLATES, TRIGGER_TEMPLATES, ACTION_TEMPLATES, END_TEMPLATES, AGENT_TEMPLATES } from "@/lib/data";
import { Pill, IconClose } from "@/components/ui";
import { agents as agentsApi, tools as toolsApi } from "@/lib/api";

interface InspectorProps {
  selected: WorkflowNode | null;
  deployed: boolean;
  workflowId: string;
  onUpdate: (n: WorkflowNode) => void;
  onDelete: () => void;
}

export function Inspector({ selected, deployed, workflowId, onUpdate, onDelete }: InspectorProps) {
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
        {selected.type === "agent"    && <AgentInspector    node={selected} deployed={deployed} workflowId={workflowId} onUpdate={onUpdate} />}
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

// ── Generic per-connector fields (Secrets/Config maps) ─────────────────────
function SecretField({
  node, onUpdate, secretKey, label, hint, placeholder,
}: {
  node: WorkflowNode;
  onUpdate: (n: WorkflowNode) => void;
  secretKey: string;
  label: string;
  hint?: string;
  placeholder: string;
}) {
  const val = node.secrets?.[secretKey];
  const isSet = val === "__enc__";
  return (
    <Field label={label} hint={hint ?? "encrypted at rest"}>
      <input
        style={monoInputStyle}
        type="password"
        value={isSet ? "" : (val ?? "")}
        placeholder={isSet ? "Key set — enter to replace" : placeholder}
        onChange={(e) => {
          const next = e.target.value || (isSet ? "__enc__" : "");
          onUpdate({ ...node, secrets: { ...node.secrets, [secretKey]: next } });
        }}
      />
    </Field>
  );
}

function ConfigField({
  node, onUpdate, configKey, label, placeholder,
}: {
  node: WorkflowNode;
  onUpdate: (n: WorkflowNode) => void;
  configKey: string;
  label: string;
  placeholder?: string;
}) {
  return (
    <Field label={label}>
      <input
        style={monoInputStyle}
        value={node.config?.[configKey] ?? ""}
        placeholder={placeholder}
        onChange={(e) => onUpdate({ ...node, config: { ...node.config, [configKey]: e.target.value } })}
      />
    </Field>
  );
}

const iconBtnStyle: React.CSSProperties = {
  width: 28, height: 28, display: "inline-flex", alignItems: "center", justifyContent: "center",
  background: "transparent", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)",
  color: "var(--fg-muted)", cursor: "pointer", fontSize: 12, fontFamily: "var(--font-mono)",
  flexShrink: 0,
};

const inputStyle: React.CSSProperties = {
  height: 36, padding: "0 10px", width: "100%",
  background: "var(--bg)", border: "1px solid var(--border)",
  borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 12,
  fontFamily: "var(--font-sans)", outline: "none",
};

const monoInputStyle: React.CSSProperties = { ...inputStyle, fontFamily: "var(--font-mono)", fontSize: 11 };

// ── Agent Inspector ────────────────────────────────────────────────────────
function AgentInspector({ node, deployed, workflowId, onUpdate }: { node: WorkflowNode; deployed: boolean; workflowId: string; onUpdate: (n: WorkflowNode) => void }) {
  const [copied, setCopied] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const copyAddress = useCallback(() => {
    if (!node.wallet) return;
    navigator.clipboard.writeText(node.wallet).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    });
  }, [node.wallet]);

  const refreshBalance = useCallback(async () => {
    if (!node.wallet || !workflowId) return;
    setRefreshing(true);
    try {
      const res = await agentsApi.balance(workflowId, node.id);
      onUpdate({ ...node, balance: res.balance });
    } catch {
      // balance fetch failed silently — keep existing value
    } finally {
      setRefreshing(false);
    }
  }, [node, workflowId, onUpdate]);

  const shortAddr = node.wallet
    ? `${node.wallet.slice(0, 6)}…${node.wallet.slice(-4)}`
    : "";

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
            {/* Network badge + address header */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
              <Pill mono dot tone="ok">algorand testnet</Pill>
              <button onClick={copyAddress} title="Copy full address" style={iconBtnStyle}>
                {copied ? "✓" : "⎘"}
              </button>
            </div>

            {/* Full address — monospace, selectable, wrapped cleanly */}
            <div style={{
              fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)",
              background: "var(--bg-elev-2)", border: "1px solid var(--border)",
              borderRadius: 6, padding: "8px 10px",
              wordBreak: "break-all", lineHeight: 1.7,
              userSelect: "text", cursor: "text",
            }}>{node.wallet}</div>

            {/* Balance row */}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: 14 }}>
              <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 28, fontWeight: 600, color: "var(--accent)", letterSpacing: "-0.02em" }}>{node.balance ?? "0.000000"}</span>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>ALGO</span>
              </div>
              <button onClick={refreshBalance} disabled={refreshing} title="Refresh balance from chain" style={{ ...iconBtnStyle, fontSize: 16, width: 32, height: 32 }}>
                <span style={{ display: "inline-block", transition: "transform 0.4s", transform: refreshing ? "rotate(360deg)" : "none" }}>↻</span>
              </button>
            </div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 2 }}>
              spent {node.spent ?? "0.000000"} ALGO · last 24h
            </div>

            {/* Fund hint */}
            <div style={{ marginTop: 12, padding: "8px 10px", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: 6, fontSize: 11, color: "var(--fg-dim)", lineHeight: 1.55 }}>
              Copy the address above and fund it via the{" "}
              <a href="https://bank.testnet.algorand.network/" target="_blank" rel="noreferrer" style={{ color: "var(--accent)", textDecoration: "none" }}>Algorand faucet</a>
              {" "}or Lora testnet. Hit ↻ to see the updated balance.
            </div>
          </div>
        ) : (
          <div style={{ padding: 14, background: "var(--bg)", border: "1px dashed var(--border-strong)", borderRadius: "var(--r-2)", fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.55 }}>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 8 }}>not yet deployed</div>
            This agent will receive an Algorand keypair on testnet when you click <strong style={{ color: "var(--fg)" }}>Deploy</strong>.
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
            : node.template === "gemini"
              ? <select style={monoInputStyle} value={node.model ?? "gemini-2.5-flash"} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value="gemini-2.5-flash">gemini-2.5-flash</option>
                  <option value="gemini-2.5-pro">gemini-2.5-pro</option>
                  <option value="gemini-2.0-flash">gemini-2.0-flash</option>
                  <option value="gemini-1.5-pro">gemini-1.5-pro</option>
                  <option value="gemini-1.5-flash">gemini-1.5-flash</option>
                </select>
              : node.template === "openai"
              ? <select style={monoInputStyle} value={node.model ?? "gpt-4.1"} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value="gpt-4.1">gpt-4.1</option>
                  <option value="gpt-4o">gpt-4o</option>
                  <option value="gpt-4o-mini">gpt-4o-mini</option>
                  <option value="o3">o3</option>
                  <option value="o4-mini">o4-mini</option>
                </select>
              : node.template === "anthropic"
              ? <select style={monoInputStyle} value={node.model ?? "claude-sonnet-4-6"} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value="claude-sonnet-4-6">claude-sonnet-4-6</option>
                  <option value="claude-opus-4-8">claude-opus-4-8</option>
                  <option value="claude-haiku-4-5">claude-haiku-4-5</option>
                  <option value="claude-3-5-sonnet-20241022">claude-3-5-sonnet</option>
                </select>
              : node.template === "groq"
              ? <select style={monoInputStyle} value={node.model ?? "llama-3.3-70b-versatile"} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value="llama-3.3-70b-versatile">llama-3.3-70b-versatile</option>
                  <option value="llama-3.1-8b-instant">llama-3.1-8b-instant</option>
                  <option value="mixtral-8x7b-32768">mixtral-8x7b</option>
                  <option value="gemma2-9b-it">gemma2-9b</option>
                </select>
              : node.template === "mistral"
              ? <select style={monoInputStyle} value={node.model ?? "mistral-large-latest"} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value="mistral-large-latest">mistral-large</option>
                  <option value="mistral-medium-latest">mistral-medium</option>
                  <option value="mistral-small-latest">mistral-small</option>
                  <option value="codestral-latest">codestral</option>
                </select>
              : <select style={monoInputStyle} value={node.model ?? tpl?.model ?? ""} onChange={(e) => onUpdate({ ...node, model: e.target.value })}>
                  <option value={tpl?.model}>{tpl?.model}</option>
                </select>}
        </Field>
      </Section>
      <Section label="Credentials">
        <Field label="API Key" hint="encrypted at rest">
          <input
            style={monoInputStyle}
            type="password"
            value={node.apiKey === "__enc__" ? "" : (node.apiKey ?? "")}
            placeholder={node.apiKey === "__enc__" ? "Key set — enter to replace" : "AIza···"}
            onChange={(e) => onUpdate({ ...node, apiKey: e.target.value || (node.apiKey === "__enc__" ? "__enc__" : "") })}
          />
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
  const [probeError, setProbeError] = useState<string | null>(null);
  const magenta = "#E879F9";

  const discover = async () => {
    if (!draft.trim()) return;
    setProbing(true);
    setProbeError(null);
    try {
      const quote = await toolsApi.x402quote(draft.trim());
      let host = draft;
      try { host = new URL(draft).host; } catch { /* use raw draft */ }
      onUpdate({
        ...node,
        endpoint: draft.trim(),
        price: quote.price ?? "?",
        unit: quote.unit ?? "call",
        provider: host,
        priceLive: true,
        description: node.description || quote.description || "",
        discoveredParams: quote.params ?? [],
      });
    } catch (err: unknown) {
      setProbeError(err instanceof Error ? err.message : "probe failed");
      onUpdate({ ...node, endpoint: draft.trim(), priceLive: false });
    } finally {
      setProbing(false);
    }
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
        <Section label="Tool description">
          <Field label="What this tool does" hint="shown to agent">
            <textarea style={{ ...inputStyle, height: "auto", padding: 10, resize: "vertical", lineHeight: 1.5 }} rows={3}
              value={node.description ?? ""}
              placeholder="Describe what this x402 endpoint provides so the agent knows when to use it…"
              onChange={(e) => onUpdate({ ...node, description: e.target.value })} />
          </Field>
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
          {probing ? <>● fetching price…</> : (node.price ? "Re-test & refresh price" : "Test endpoint & fetch price")}
        </button>
        {probeError && (
          <div style={{ padding: "8px 10px", background: "rgba(248,113,113,0.08)", border: "1px solid rgba(248,113,113,0.3)", borderRadius: "var(--r-2)", fontFamily: "var(--font-mono)", fontSize: 11, color: "#F87171" }}>
            {probeError}
          </div>
        )}
        {node.price && !probing && (
          <div style={{ padding: 14, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", fontFamily: "var(--font-mono)", fontSize: 11 }}>
            <div style={{ color: "var(--fg-muted)" }}>{node.provider}</div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginTop: 12 }}>
              <span style={{ color: magenta, fontSize: 22, fontWeight: 500 }}>{node.price}</span>
              <span style={{ color: "var(--fg-muted)" }}>ALGO / {node.unit}</span>
            </div>
            <div style={{ marginTop: 8, color: node.priceLive ? "var(--accent)" : "var(--fg-dim)" }}>
              {node.priceLive ? "● live · fetched from backend" : "● cached · endpoint unreachable"}
            </div>
          </div>
        )}
      </Section>
      {node.discoveredParams && node.discoveredParams.length > 0 && (
        <Section label="Endpoint params">
          <div style={{ fontSize: 11, color: "var(--fg-dim)", marginBottom: 6 }}>
            Filled automatically by the agent at runtime.
          </div>
          <div style={{ background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", overflow: "hidden" }}>
            {node.discoveredParams.map((p, i) => (
              <div key={p.name} style={{ padding: "8px 12px", borderBottom: i < node.discoveredParams!.length - 1 ? "1px solid var(--border)" : "none", display: "flex", alignItems: "flex-start", gap: 8 }}>
                <div style={{ flex: 1 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 2 }}>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: magenta }}>{p.name}</span>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", background: "var(--bg-elev-2)", padding: "1px 5px", borderRadius: 3 }}>{p.type}</span>
                    {p.required
                      ? <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "#F87171" }}>required</span>
                      : <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)" }}>optional</span>}
                  </div>
                  <div style={{ fontSize: 10, color: "var(--fg-muted)", lineHeight: 1.4 }}>{p.description}{p.default ? ` · default: ${p.default}` : ""}</div>
                </div>
              </div>
            ))}
          </div>
        </Section>
      )}
      <Section label="Tool description">
        <Field label="What this tool does" hint="shown to agent">
          <textarea style={{ ...inputStyle, height: "auto", padding: 10, resize: "vertical", lineHeight: 1.5 }} rows={3}
            value={node.description ?? ""}
            placeholder="Describe what this x402 endpoint provides so the agent knows when to use it…"
            onChange={(e) => onUpdate({ ...node, description: e.target.value })} />
        </Field>
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
    <>
      <Section label="Action">
        <Field label="Name">
          <input style={inputStyle} value={node.name ?? ""} onChange={(e) => onUpdate({ ...node, name: e.target.value })} />
        </Field>
      </Section>

      {node.template === "email" && (
        <Section label="Email config">
          <Field label="Provider">
            <select style={inputStyle} value={node.emailProvider ?? "resend"} onChange={(e) => onUpdate({ ...node, emailProvider: e.target.value })}>
              <option value="resend">Resend</option>
              <option value="postmark">Postmark</option>
              <option value="sendgrid">SendGrid</option>
            </select>
          </Field>
          <Field label="API Key" hint="encrypted at rest">
            <input style={monoInputStyle} type="password"
              value={node.emailApiKey === "__enc__" ? "" : (node.emailApiKey ?? "")}
              placeholder={node.emailApiKey === "__enc__" ? "Key set — enter to replace" : (node.emailProvider === "postmark" ? "your-postmark-server-token" : "re_xxxxxxxxxxxx")}
              onChange={(e) => onUpdate({ ...node, emailApiKey: e.target.value || (node.emailApiKey === "__enc__" ? "__enc__" : "") })} />
          </Field>
          <Field label="From" hint="must be verified in your provider">
            <input style={monoInputStyle}
              value={node.emailFrom ?? ""}
              placeholder="AgentMesh <you@yourdomain.com>"
              onChange={(e) => onUpdate({ ...node, emailFrom: e.target.value })} />
          </Field>
          <Field label="To" hint="{{ variables }} supported">
            <input style={monoInputStyle}
              value={node.emailTo ?? ""}
              placeholder="recipient@example.com"
              onChange={(e) => onUpdate({ ...node, emailTo: e.target.value })} />
          </Field>
          <Field label="Subject">
            <input style={inputStyle}
              value={node.emailSubject ?? ""}
              placeholder="Your AgentMesh result"
              onChange={(e) => onUpdate({ ...node, emailSubject: e.target.value })} />
          </Field>
          <Field label="Body" hint="{{ result }} = agent output">
            <textarea style={{ ...inputStyle, height: "auto", padding: 10, resize: "vertical", lineHeight: 1.6 }} rows={5}
              value={node.emailBody ?? ""}
              placeholder={"Hi,\n\nHere is your result:\n\n{{ result }}\n\n— AgentMesh"}
              onChange={(e) => onUpdate({ ...node, emailBody: e.target.value })} />
          </Field>
        </Section>
      )}

      {node.template === "slack" && (
        <Section label="Slack config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="slackWebhookURL" label="Webhook URL"
            placeholder="https://hooks.slack.com/services/…" />
        </Section>
      )}

      {node.template === "discord" && (
        <Section label="Discord config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="discordWebhookURL" label="Webhook URL"
            placeholder="https://discord.com/api/webhooks/…" />
        </Section>
      )}

      {node.template === "teams" && (
        <Section label="Teams config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="teamsWebhookURL" label="Webhook URL"
            placeholder="https://…webhook.office.com/webhookb2/…" />
        </Section>
      )}

      {node.template === "google_chat" && (
        <Section label="Google Chat config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="googleChatWebhookURL" label="Webhook URL"
            placeholder="https://chat.googleapis.com/v1/spaces/…" />
        </Section>
      )}

      {node.template === "ntfy" && (
        <Section label="Ntfy config">
          <ConfigField node={node} onUpdate={onUpdate} configKey="ntfyTopic" label="Topic" placeholder="agentmesh-alerts" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="ntfyServerURL" label="Server URL" placeholder="https://ntfy.sh (default)" />
          <SecretField node={node} onUpdate={onUpdate} secretKey="ntfyAuthToken" label="Auth Token" hint="optional, for private topics"
            placeholder="tk_xxxxxxxxxxxx" />
        </Section>
      )}

      {node.template === "telegram" && (
        <Section label="Telegram config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="telegramBotToken" label="Bot Token"
            placeholder="123456789:AAExxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="telegramChatID" label="Chat ID" placeholder="-1001234567890" />
        </Section>
      )}

      {node.template === "github" && (
        <Section label="GitHub config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="githubToken" label="Personal Access Token"
            placeholder="ghp_xxxxxxxxxxxxxxxxxxxx" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="githubRepo" label="Repository" placeholder="owner/repo" />
        </Section>
      )}

      {node.template === "notion" && (
        <Section label="Notion config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="notionAPIKey" label="Internal Integration Secret"
            placeholder="secret_xxxxxxxxxxxxxxxxxxxx" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="notionPageID" label="Page ID" placeholder="the target page's UUID" />
        </Section>
      )}

      {node.template === "airtable" && (
        <Section label="Airtable config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="airtableAPIKey" label="Personal Access Token"
            placeholder="pat_xxxxxxxxxxxxxxxxxxxx" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="airtableBaseID" label="Base ID" placeholder="appXXXXXXXXXXXXXX" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="airtableTable" label="Table" placeholder="Tasks" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="airtableFieldName" label="Field Name" placeholder="Notes (default)" />
        </Section>
      )}

      {node.template === "hubspot" && (
        <Section label="HubSpot config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="hubspotAPIKey" label="Private App Token"
            placeholder="pat-na1-xxxxxxxxxxxxxxxxxxxx" />
        </Section>
      )}

      {node.template === "trello" && (
        <Section label="Trello config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="trelloAPIKey" label="API Key" placeholder="your Trello API key" />
          <SecretField node={node} onUpdate={onUpdate} secretKey="trelloToken" label="Token" placeholder="your Trello token" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="trelloListID" label="List ID" placeholder="target list id" />
        </Section>
      )}

      {node.template === "asana" && (
        <Section label="Asana config">
          <SecretField node={node} onUpdate={onUpdate} secretKey="asanaAPIKey" label="Personal Access Token"
            placeholder="1/1234567890:xxxxxxxxxxxxxxxxxxxx" />
          <ConfigField node={node} onUpdate={onUpdate} configKey="asanaProjectID" label="Project ID" placeholder="target project id" />
        </Section>
      )}
    </>
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
