"use client";
import { useEffect, useState, useSyncExternalStore } from "react";
import { BASE, workflows as workflowsApi } from "@/lib/api";
import type { Workflow } from "@/lib/types";
import {
  SettingRow,
  SettingsSection,
  inputStyle,
} from "@/components/settings/ui";
import { ghostBtnSm } from "@/components/ui";

// The origin cannot change for the life of the page, so the subscription is
// a no-op; module scope keeps the reference stable across renders.
const subscribeToOrigin = () => () => {};

// The trigger endpoint has been live since the first router (POST /run/{id})
// and has never been shown anywhere in the UI. This section is purely a
// read-only window onto it — nothing here writes.
export function DeveloperSection() {
  const [list, setList] = useState<Workflow[]>([]);
  const [selected, setSelected] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let live = true;
    workflowsApi
      .list()
      .then((wfs) => {
        if (!live) return;
        setList(wfs);
        setSelected((s) => s || wfs[0]?.id || "");
      })
      // A failed workflow list must not break the rest of settings; the
      // section just falls back to the placeholder URL below.
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);

  // BASE is "/api" in the browser when a backend is configured, so resolve it
  // against the origin to show a URL someone can actually paste into curl.
  //
  // Set after mount rather than read during render: the server has no window
  // and emits "", so reading it in render hydrates a different URL than the
  // one the server sent.
  // useSyncExternalStore rather than state-in-effect: React reads the server
  // snapshot ("") during hydration, so the first client paint matches the
  // HTML the server sent, then swaps in the real origin.
  const origin = useSyncExternalStore(
    subscribeToOrigin,
    () => window.location.origin,
    () => "",
  );
  const root = BASE.startsWith("http") ? BASE : `${origin}${BASE}`;
  const url = `${root}/run/${selected || "{workflowId}"}`;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      /* clipboard blocked (insecure origin / denied) — the field is selectable */
    }
  };

  return (
    <SettingsSection
      id="developer"
      title="Developer"
      description="Trigger a deployed workflow from anything that can send an HTTP request."
    >
      <SettingRow
        label="Workflow"
        htmlFor="set-trigger-workflow"
        hint="Pick a workflow to see its trigger URL."
      >
        <select
          id="set-trigger-workflow"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
          disabled={list.length === 0}
          style={{ ...inputStyle, maxWidth: 320 }}
        >
          {list.length === 0 && <option value="">No workflows yet</option>}
          {list.map((wf) => (
            <option key={wf.id} value={wf.id}>
              {wf.name}
            </option>
          ))}
        </select>
      </SettingRow>

      <SettingRow
        label="Trigger endpoint"
        hint="This endpoint is public by design — anyone holding the URL can start a run, and runs spend your credits. Treat it like a secret."
      >
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <input
            readOnly
            value={`POST ${url}`}
            onFocus={(e) => e.currentTarget.select()}
            aria-label="Public trigger endpoint"
            style={{
              ...inputStyle,
              fontFamily: "var(--font-mono)",
              color: "var(--fg-muted)",
            }}
          />
          <button
            type="button"
            onClick={copy}
            className="set-copy"
            style={{ ...ghostBtnSm, flexShrink: 0, minWidth: 68 }}
          >
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
      </SettingRow>
    </SettingsSection>
  );
}
