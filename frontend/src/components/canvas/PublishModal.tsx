"use client";
import { useState } from "react";
import { marketplace as marketplaceApi } from "@/lib/api";

interface Props {
  workflowId: string;
  workflowName: string;
  onClose: () => void;
  onPublished: () => void;
}

export function PublishModal({ workflowId, workflowName, onClose, onPublished }: Props) {
  const [title, setTitle] = useState(workflowName);
  const [description, setDescription] = useState("");
  const [tagsRaw, setTagsRaw] = useState("");
  const [feePerRun, setFeePerRun] = useState("0");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const valid = title.trim().length >= 2;

  const handleSubmit = async () => {
    if (!valid || loading) return;
    setLoading(true);
    setError(null);
    try {
      const tags = tagsRaw.split(",").map((t) => t.trim()).filter(Boolean);
      const fee = parseFloat(feePerRun) || 0;
      await marketplaceApi.publishWorkflow(workflowId, title.trim(), description.trim(), tags, fee);
      onPublished();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "publish failed");
      setLoading(false);
    }
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.75)", backdropFilter: "blur(4px)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 480, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 600, color: "var(--fg)" }}>Publish to Marketplace</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 3 }}>Others can discover and import this workflow · you earn 1% of each run</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <Field label="Title" value={title} onChange={setTitle} placeholder="e.g. Lead research agent" />
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <label style={labelStyle}>Description</label>
            <textarea
              value={description} onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this workflow do? What inputs does it need?"
              style={{ width: "100%", minHeight: 80, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
            />
          </div>
          <Field label="Tags (comma-separated)" value={tagsRaw} onChange={setTagsRaw} placeholder="research, email, leads" />
          <Field label="Fee per run (USD)" value={feePerRun} onChange={setFeePerRun} placeholder="0" type="number" />
        </div>

        {error && <div style={{ fontSize: 12, color: "#f87171", fontFamily: "var(--font-mono)" }}>{error}</div>}

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={ghostBtnStyle}>Cancel</button>
          <button onClick={handleSubmit} disabled={!valid || loading} style={{ ...primaryBtnStyle, opacity: valid && !loading ? 1 : 0.5, cursor: valid && !loading ? "pointer" : "default" }}>
            {loading ? "Publishing…" : "Publish"}
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, type = "text" }: { label: string; value: string; onChange: (v: string) => void; placeholder: string; type?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <label style={labelStyle}>{label}</label>
      <input
        type={type} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder}
        style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }}
      />
    </div>
  );
}

const labelStyle: React.CSSProperties = { fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" };
const primaryBtnStyle: React.CSSProperties = { height: 32, padding: "0 16px", fontSize: 12, fontWeight: 600, background: "var(--accent)", border: "1px solid var(--accent)", borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer", fontFamily: "var(--font-sans)" };
const ghostBtnStyle: React.CSSProperties = { height: 32, padding: "0 14px", fontSize: 12, fontWeight: 500, background: "transparent", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer", fontFamily: "var(--font-sans)" };
