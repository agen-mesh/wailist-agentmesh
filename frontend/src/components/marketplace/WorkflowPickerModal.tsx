"use client";
import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { workflows as workflowsApi } from "@/lib/api";
import type { MarketplaceEndpoint } from "@/lib/types";

interface Props {
  endpoint: MarketplaceEndpoint;
  onClose: () => void;
}

export function WorkflowPickerModal({ endpoint, onClose }: Props) {
  const router = useRouter();
  const [items, setItems] = useState<{ id: string; name: string }[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    workflowsApi
      .list()
      .then((wfs) => setItems(wfs.map((w) => ({ id: w.id, name: w.name }))))
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  }, []);

  const pick = (workflowId: string) => {
    localStorage.setItem(
      "agentmesh:pendingNode",
      JSON.stringify({
        type: "tool402",
        name: endpoint.name,
        endpoint: endpoint.endpoint ?? "",
        description: endpoint.description,
        discoveredParams: endpoint.discoveredParams ?? [],
      }),
    );
    router.push(`/workflows/${workflowId}`);
    onClose();
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.7)", backdropFilter: "blur(4px)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 420, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: "24px", display: "flex", flexDirection: "column", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>Add to workflow</div>
            <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", marginTop: 2 }}>{endpoint.name}</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {loading && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "16px 0", textAlign: "center" }}>
              loading workflows…
            </div>
          )}
          {!loading && items.length === 0 && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "16px 0", textAlign: "center" }}>
              No workflows yet — create one first.
            </div>
          )}
          {items.map((wf) => (
            <button
              key={wf.id}
              onClick={() => pick(wf.id)}
              style={{ textAlign: "left", padding: "10px 14px", background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, cursor: "pointer", fontFamily: "var(--font-sans)" }}
            >
              {wf.name}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
