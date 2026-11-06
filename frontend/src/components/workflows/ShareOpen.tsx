"use client";
import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { openSharedWorkflow } from "@/lib/arkivShare";
import { workflowHref } from "@/lib/routes";
import { ShareKeyError } from "@/lib/shareCrypto";
import { loadTemplateWorkflow } from "@/lib/templateWorkflow";
import type { Workflow } from "@/lib/types";
import type { WorkflowShareData } from "@/lib/workflowShare";

const authorship =
  "Signed by AgentMesh. This link does not prove which person shared it.";
const death =
  "This link dies 7 days after it is confirmed. AgentMesh does not keep a copy. A retired Arkiv testnet kills the link too.";
const keyError = new ShareKeyError().message;

export function ShareOpen({ entityKey, hash }: { entityKey: string; hash: string }) {
  const [data, setData] = useState<WorkflowShareData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const opened = await openSharedWorkflow(entityKey, hash);
        if (!cancelled) setData(opened);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : "could not open share");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [entityKey, hash]);

  const save = async () => {
    if (!data || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      const id = await loadTemplateWorkflow({
        id: entityKey,
        name: data.name ?? "Shared workflow",
        nodes: data.nodes as Workflow["nodes"],
        edges: data.edges as Workflow["edges"],
      });
      window.location.href = workflowHref(id);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "could not save");
    } finally {
      setSaving(false);
    }
  };

  const showAuthorship = data !== null || error === keyError;

  return (
    <main style={{ maxWidth: 640, margin: "0 auto", padding: 24, color: "var(--fg)" }}>
      <h1 style={{ fontSize: "var(--t-5)", margin: "0 0 8px" }}>{data?.name ?? "Shared workflow"}</h1>
      {showAuthorship && (
        <p style={{ margin: "0 0 8px", color: "var(--fg-muted)" }}>{authorship}</p>
      )}
      <p style={{ margin: "0 0 16px", color: "var(--fg-muted)" }}>{death}</p>
      {data && (
        <p style={{ margin: "0 0 16px" }}>
          <span>{data.nodes.length} nodes</span>
          <span>{data.edges.length} edges</span>
        </p>
      )}
      {error && <p style={{ color: "var(--danger)" }}>{error}</p>}
      {saveError && <p style={{ color: "var(--danger)" }}>{saveError}</p>}
      {data && (
        <button
          type="button"
          onClick={save}
          disabled={saving}
          style={{
            height: 38,
            padding: "0 16px",
            borderRadius: "var(--r-2)",
            border: "1px solid var(--accent-line)",
            background: "var(--accent)",
            color: "var(--accent-fg)",
            fontWeight: 600,
            cursor: saving ? "default" : "pointer",
          }}
        >
          Save to my workflows
        </button>
      )}
    </main>
  );
}

export function ShareRoute({ routeKey }: { routeKey: string }) {
  const fromQuery = useSearchParams().get("key");
  const [hash, setHash] = useState<string | null>(null);
  useEffect(() => {
    setHash(window.location.hash);
  }, []);
  if (hash === null) return null;
  const entityKey = routeKey === "app" && fromQuery ? fromQuery : routeKey;
  return <ShareOpen entityKey={entityKey} hash={hash} />;
}
