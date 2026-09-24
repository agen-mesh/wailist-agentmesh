"use client";
import { Card } from "@/components/ui";
import { primaryBtn } from "@/components/ui/buttons";

// Shown by the app's launch page when the session check could not reach the
// server. Before this, a phone that was offline, or a server that was down,
// looked exactly like being signed out and landed on the sign-in screen.
export function OfflineScreen({
  retrying,
  onRetry,
}: {
  retrying: boolean;
  onRetry: () => void;
}) {
  return (
    <main style={page}>
      <Card style={card}>
        <h1 style={title}>Can’t reach AgentMesh</h1>
        <p style={copy} aria-live="polite">
          {retrying
            ? "Trying again…"
            : "Check your connection, then try again."}
        </p>
        <button
          type="button"
          onClick={onRetry}
          disabled={retrying}
          style={{
            ...primaryBtn,
            width: "100%",
            minHeight: 44,
            justifyContent: "center",
            opacity: retrying ? 0.6 : 1,
          }}
        >
          {retrying ? "Retrying…" : "Retry"}
        </button>
      </Card>
    </main>
  );
}

const page: React.CSSProperties = {
  minHeight: "100dvh",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  background: "var(--bg)",
  padding:
    "calc(24px + var(--safe-top)) calc(16px + var(--safe-right)) calc(24px + var(--safe-bottom)) calc(16px + var(--safe-left))",
};

const card: React.CSSProperties = {
  width: "100%",
  maxWidth: 360,
  padding: 24,
  display: "grid",
  gap: 12,
};

const title: React.CSSProperties = {
  margin: 0,
  font: "600 18px/1.3 var(--font-sans)",
  color: "var(--fg)",
};

const copy: React.CSSProperties = {
  margin: 0,
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg-muted)",
};
