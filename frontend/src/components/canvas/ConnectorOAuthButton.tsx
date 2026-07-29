"use client";

import type { WorkflowNode } from "@/lib/types";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL && typeof window !== "undefined"
    ? "/api"
    : (process.env.NEXT_PUBLIC_API_URL ?? "");

export function ConnectorOAuthButton({
  provider,
  workflowId,
  node,
}: {
  provider: string;
  workflowId: string;
  node: WorkflowNode;
}) {
  const secretKey = `${provider}OAuthAccessToken`;
  const connected = node.secrets?.[secretKey] === "__enc__";

  const connect = () => {
    if (!API_BASE) return; // mock-data mode has no backend to redirect to
    window.location.href = `${API_BASE}/connectors/oauth/${provider}/start?workflowId=${encodeURIComponent(
      workflowId,
    )}&nodeId=${encodeURIComponent(node.id)}`;
  };

  if (connected) {
    return (
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ opacity: 0.8 }}>Connected</span>
        <button type="button" onClick={connect} style={{ opacity: 0.6 }}>
          Reconnect
        </button>
      </div>
    );
  }

  return (
    <button type="button" onClick={connect}>
      Connect
    </button>
  );
}
