"use client";
import { useState } from "react";
import { IconClose } from "@/components/ui";
import { useModalDismissal } from "@/hooks/useModalDismissal";
import { loadTemplateWorkflow } from "@/lib/templateWorkflow";
import type { WorkflowEdge, WorkflowNode } from "@/lib/types";
import { decodeWorkflowShare } from "@/lib/workflowShare";

const IconPaste = ({ size = 13 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect
      x="4"
      y="2.5"
      width="8"
      height="11"
      rx="1.3"
      stroke="currentColor"
      strokeWidth="1.3"
    />
    <path
      d="M6.2 2.5V2a1 1 0 0 1 1-1h1.6a1 1 0 0 1 1 1v.5"
      stroke="currentColor"
      strokeWidth="1.3"
    />
  </svg>
);

// Mounted only while open (the parent renders it conditionally on
// importOpen, the same pattern AddToWorkflowDialog uses) -- so each open is
// a fresh mount and a fresh slate, with no reset-on-open effect needed.
export function ImportModal({
  onClose,
  onImported,
}: {
  onClose: () => void;
  onImported: (id: string) => void;
}) {
  const [code, setCode] = useState("");
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useModalDismissal(onClose);

  const handlePasteFromClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      setCode(text);
      setError(null);
    } catch {
      setError(
        "clipboard read was blocked -- paste manually with Ctrl+V (Cmd+V on Mac)",
      );
    }
  };

  const handleImport = async () => {
    if (!code.trim() || importing) return;
    setImporting(true);
    setError(null);
    try {
      const data = await decodeWorkflowShare(code);
      const id = await loadTemplateWorkflow({
        id: "",
        name: data.name?.trim() || "Imported workflow",
        nodes: data.nodes as WorkflowNode[],
        edges: data.edges as WorkflowEdge[],
      });
      onImported(id);
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "could not import that code -- make sure it's a full workflow share code",
      );
      setImporting(false);
    }
  };

  return (
    <div
      role="presentation"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 1000,
        background: "rgba(8,7,12,0.72)",
        backdropFilter: "blur(4px)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 24,
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Import workflow"
        style={{
          width: "100%",
          maxWidth: 480,
          border: "1px solid var(--border-strong)",
          borderRadius: "var(--r-4)",
          background: "var(--bg-elev-1)",
          color: "var(--fg)",
          boxShadow: "0 24px 64px rgba(0,0,0,0.55)",
          padding: 24,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "flex-start",
            justifyContent: "space-between",
            marginBottom: 16,
          }}
        >
          <div>
            <h2
              style={{
                fontSize: "var(--t-5)",
                fontWeight: 700,
                margin: 0,
                letterSpacing: "-0.01em",
              }}
            >
              Import workflow
            </h2>
            <p style={{ margin: "3px 0 0", fontSize: "var(--t-2)", color: "var(--fg-muted)" }}>
              Paste a code from someone&apos;s Share
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            style={{
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              width: 30,
              height: 30,
              background: "transparent",
              border: "1px solid var(--border)",
              borderRadius: "var(--r-2)",
              color: "var(--fg-muted)",
              cursor: "pointer",
            }}
          >
            <IconClose size={13} />
          </button>
        </div>

        <textarea
          autoFocus
          value={code}
          onChange={(e) => {
            setCode(e.target.value);
            setError(null);
          }}
          placeholder="Paste the code here, or use Paste from clipboard below…"
          style={{
            width: "100%",
            height: 110,
            resize: "none",
            fontFamily: "var(--font-mono)",
            fontSize: "var(--t-1)",
            lineHeight: 1.5,
            padding: 10,
            background: "var(--bg-elev-2)",
            border: "1px solid var(--border)",
            borderRadius: "var(--r-2)",
            color: "var(--fg)",
            marginBottom: 10,
            wordBreak: "break-all",
          }}
        />

        <button
          type="button"
          onClick={handlePasteFromClipboard}
          style={{
            width: "100%",
            height: 34,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: "var(--s-2)",
            borderRadius: "var(--r-2)",
            border: "1px solid var(--border-strong)",
            background: "transparent",
            color: "var(--fg)",
            fontSize: "var(--t-2)",
            fontWeight: 500,
            cursor: "pointer",
            marginBottom: 14,
          }}
        >
          <IconPaste /> Paste from clipboard
        </button>

        {error && (
          <div style={{ fontSize: "var(--t-2)", color: "var(--danger)", marginBottom: 12 }}>
            {error}
          </div>
        )}

        <div style={{ display: "flex", gap: "var(--s-3)" }}>
          <button
            type="button"
            onClick={onClose}
            style={{
              flex: 1,
              height: 38,
              borderRadius: "var(--r-2)",
              border: "1px solid var(--border-strong)",
              background: "transparent",
              color: "var(--fg-muted)",
              fontSize: "var(--t-3)",
              fontWeight: 500,
              cursor: "pointer",
            }}
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={!code.trim() || importing}
            onClick={handleImport}
            style={{
              flex: 1,
              height: 38,
              borderRadius: "var(--r-2)",
              border: "1px solid var(--accent-line)",
              background: "var(--accent)",
              color: "var(--accent-fg)",
              fontSize: "var(--t-3)",
              fontWeight: 600,
              cursor: !code.trim() || importing ? "not-allowed" : "pointer",
              opacity: !code.trim() || importing ? 0.6 : 1,
            }}
          >
            {importing ? "Importing…" : "Import"}
          </button>
        </div>
      </div>
    </div>
  );
}
