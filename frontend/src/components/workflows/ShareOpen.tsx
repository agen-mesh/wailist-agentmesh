"use client";
import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useAuth } from "@/hooks/useAuth";
import { useReadOnly } from "@/hooks/useReadOnly";
import { openSharedWorkflow } from "@/lib/arkivShare";
import { workflowHref } from "@/lib/routes";
import { ShareKeyError } from "@/lib/shareCrypto";
import { stripShareGraph } from "@/lib/sharePayload";
import { loadTemplateWorkflow } from "@/lib/templateWorkflow";
import type { Workflow } from "@/lib/types";
import type { WorkflowShareData } from "@/lib/workflowShare";
import styles from "./ShareOpen.module.css";

const authorship =
  "Signed by AgentMesh. This link does not prove which person shared it.";
const death =
  "This link dies 7 days after it is confirmed. AgentMesh does not keep a copy. A retired Arkiv testnet kills the link too.";
const keyError = new ShareKeyError().message;
const computerOnly = "Open this link on a computer to save it.";

function shareKeyName(entityKey: string): string {
  return `arkiv-share-k:${entityKey}`;
}

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function textField(node: object, key: string): string {
  const value = (node as Record<string, unknown>)[key];
  return typeof value === "string" ? value.trim() : "";
}

function describeNode(node: unknown): { title: string; type: string } {
  if (!node || typeof node !== "object") return { title: "Untitled step", type: "" };
  const name = textField(node, "name") || textField(node, "label");
  const type = textField(node, "type");
  return { title: name || type || "Untitled step", type: name && type ? type : "" };
}

function nodeKey(node: unknown, index: number): string {
  if (node && typeof node === "object") {
    const id = textField(node, "id");
    if (id) return id;
  }
  return String(index);
}

export function rememberShareKey(entityKey: string, hash: string) {
  const params = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
  const key = params.get("k");
  if (!key) return;
  sessionStorage.setItem(shareKeyName(entityKey), key);
}

export function hashWithStoredKey(entityKey: string, hash: string): string {
  if (new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash).get("k")) return hash;
  const key = sessionStorage.getItem(shareKeyName(entityKey));
  return key ? `#k=${encodeURIComponent(key)}` : hash;
}

export function ShareOpen({ entityKey, hash }: { entityKey: string; hash: string }) {
  const [data, setData] = useState<WorkflowShareData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const readOnly = useReadOnly();
  const { signedIn, loading: authLoading } = useAuth();

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
      const stripped = stripShareGraph({
        name: data.name,
        nodes: data.nodes,
        edges: data.edges,
      });
      const id = await loadTemplateWorkflow({
        id: entityKey,
        name: stripped.name ?? "Shared workflow",
        nodes: stripped.nodes as Workflow["nodes"],
        edges: stripped.edges as Workflow["edges"],
      });
      window.location.href = workflowHref(id);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "could not save");
    } finally {
      setSaving(false);
    }
  };

  const showAuthorship = data !== null || error === keyError;
  const showSave = data !== null && !readOnly && (authLoading || signedIn);
  const showSignIn = data !== null && !readOnly && !authLoading && !signedIn;
  const signInHref = `/signin?next=${encodeURIComponent(`/s/${entityKey}`)}`;

  const title = data?.name?.trim() || "Shared workflow";

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>{title}</h1>
      {!data && !error && <p className={styles.status}>Opening this share.</p>}
      {data && (
        <>
          <div className={styles.counts}>
            <p className={styles.count}>{countLabel(data.nodes.length, "node", "nodes")}</p>
            <p className={styles.count}>{countLabel(data.edges.length, "edge", "edges")}</p>
          </div>
          {data.nodes.length > 0 && (
            <ol className={styles.steps}>
              {data.nodes.map((node, index) => {
                const step = describeNode(node);
                return (
                  <li key={nodeKey(node, index)} className={styles.step}>
                    <span className={styles.mark} aria-hidden="true" />
                    <span className={styles.stepBody}>
                      <span className={styles.stepTitle}>{step.title}</span>
                      {step.type && <span className={styles.stepType}>{step.type}</span>}
                    </span>
                  </li>
                );
              })}
            </ol>
          )}
        </>
      )}
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {saveError && (
        <p className={styles.error} role="alert">
          {saveError}
        </p>
      )}
      {data && readOnly && <p className={styles.note}>{computerOnly}</p>}
      {(showSignIn || showSave) && (
        <div className={styles.actions}>
          {showSignIn && (
            <a className={styles.action} href={signInHref} onClick={() => rememberShareKey(entityKey, hash)}>
              Sign in to save
            </a>
          )}
          {showSave && (
            <button className={styles.action} type="button" onClick={save} disabled={saving}>
              Save to my workflows
            </button>
          )}
        </div>
      )}
      {showAuthorship && <p className={styles.note}>{authorship}</p>}
      {showAuthorship && <p className={styles.note}>{death}</p>}
    </main>
  );
}

export function ShareRoute({ routeKey }: { routeKey: string }) {
  const fromQuery = useSearchParams().get("key");
  const [hash, setHash] = useState<string | null>(null);
  useEffect(() => {
    const entityKey = routeKey === "app" && fromQuery ? fromQuery : routeKey;
    const current = window.location.hash;
    rememberShareKey(entityKey, current);
    setHash(hashWithStoredKey(entityKey, current));
  }, [routeKey, fromQuery]);
  if (hash === null) return null;
  const entityKey = routeKey === "app" && fromQuery ? fromQuery : routeKey;
  return <ShareOpen entityKey={entityKey} hash={hash} />;
}
