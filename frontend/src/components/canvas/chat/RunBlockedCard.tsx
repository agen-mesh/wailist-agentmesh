"use client";
import type { RunBlockedReason } from "../runBlocked";

/**
 * The card's presentation, split out from the component so the copy and tone
 * rules are unit-testable without a DOM. An unbuilt workflow is not a
 * failure -- it is a workflow mid-construction -- so it gets the warm tone
 * and no button; an undeployed one is a real blocker with a real fix.
 */
export function runBlockedCardCopy(reason: RunBlockedReason): {
  tone: "warn" | "error";
  actionLabel: string | null;
} {
  return {
    tone: reason.code === "not-ready" ? "warn" : "error",
    actionLabel: reason.action === "deploy" ? "Deploy now" : null,
  };
}

/**
 * Shown above the composer whenever a run cannot start. Persistent by
 * design: the old treatment was a 2.4-second toast under a green success
 * dot, which meant the one message explaining why nothing happened was gone
 * before it could be read and offered nothing to press.
 */
export function RunBlockedCard({
  reason,
  onDeploy,
  onDismiss,
  deploying,
}: {
  reason: RunBlockedReason;
  onDeploy: () => void;
  onDismiss: () => void;
  deploying?: boolean;
}) {
  const { tone, actionLabel } = runBlockedCardCopy(reason);
  const line = tone === "error" ? "var(--danger)" : "var(--warm)";
  return (
    <div
      role="alert"
      style={{
        margin: "0 10px 8px",
        padding: "10px 12px",
        borderRadius: "var(--r-2)",
        border: `1px solid ${line}`,
        background: "var(--bg-elev-2)",
        display: "flex",
        flexDirection: "column",
        gap: 8,
        minWidth: 0,
      }}
    >
      <div
        style={{ display: "flex", alignItems: "baseline", gap: 8, minWidth: 0 }}
      >
        <span
          aria-hidden
          style={{
            width: 6,
            height: 6,
            borderRadius: 999,
            background: line,
            flexShrink: 0,
            transform: "translateY(-2px)",
          }}
        />
        <div style={{ minWidth: 0 }}>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              color: line,
            }}
          >
            {reason.title}
          </div>
          <div
            style={{
              fontSize: 12.5,
              lineHeight: 1.45,
              color: "var(--fg)",
              marginTop: 3,
              overflowWrap: "anywhere",
            }}
          >
            {reason.detail}
          </div>
        </div>
      </div>
      <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
        {actionLabel && (
          <button
            onClick={onDeploy}
            disabled={deploying}
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              padding: "4px 10px",
              borderRadius: 999,
              border: `1px solid ${line}`,
              background: "transparent",
              color: line,
              cursor: deploying ? "progress" : "pointer",
              opacity: deploying ? 0.6 : 1,
            }}
          >
            {deploying ? "Deploying…" : actionLabel}
          </button>
        )}
        <button
          onClick={onDismiss}
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            textTransform: "uppercase",
            letterSpacing: "0.06em",
            padding: "4px 10px",
            borderRadius: 999,
            border: "1px solid var(--border)",
            background: "transparent",
            color: "var(--fg-dim)",
            cursor: "pointer",
          }}
        >
          Dismiss
        </button>
      </div>
    </div>
  );
}
