"use client";
import { useEffect, useState } from "react";
import { IconClose } from "@/components/ui";
import { useModalDismissal } from "@/hooks/useModalDismissal";
import { workflows as workflowsApi } from "@/lib/api";
import { encodeWorkflowShare } from "@/lib/workflowShare";

// mailto: URLs get truncated by a lot of mail clients/OSes past roughly
// 2000 total characters, so the code only goes in the email body when it's
// short enough to survive that -- otherwise the body just points back at
// the code already sitting in the user's clipboard from "Copy code".
const MAILTO_SAFE_CODE_LENGTH = 1200;

const IconCopy = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect
      x="5.5"
      y="5.5"
      width="8"
      height="8"
      rx="1.5"
      stroke="currentColor"
      strokeWidth="1.3"
    />
    <path
      d="M2.5 10.5v-7A1 1 0 0 1 3.5 2.5h7"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
    />
  </svg>
);

const IconX = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="currentColor">
    <path d="M9.3 6.9 14 2h-1.6L8.6 5.9 5.4 2H1l5 6.7L1 14h1.6l4-4.3L9.9 14H14L9.3 6.9Zm-1.4 1.6-.5-.6L3.2 3h1.4l3 4 .5.6 3.9 5.3H10.6l-2.7-3.4Z" />
  </svg>
);

const IconMail = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect
      x="1.5"
      y="3.5"
      width="13"
      height="9"
      rx="1.5"
      stroke="currentColor"
      strokeWidth="1.3"
    />
    <path
      d="M2 4.5 8 9l6-4.5"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  return `${(n / 1024).toFixed(1)} KB`;
}

// Mounted only while open (the parent renders it conditionally on
// shareWorkflowId, the same pattern AddToWorkflowDialog uses) -- so a fresh
// mount per share is the reset, and the fetch-then-encode effect never needs
// to synchronously setState before the async work starts.
export function ShareModal({
  workflowId,
  onClose,
}: {
  workflowId: string;
  onClose: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [prepared, setPrepared] = useState<{ name: string; code: string } | null>(
    null,
  );
  const [copied, setCopied] = useState(false);
  const loading = !prepared && !error;
  const name = prepared?.name ?? "";
  const code = prepared?.code ?? "";

  useModalDismissal(onClose);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const wf = await workflowsApi.get(workflowId);
        const encoded = await encodeWorkflowShare({
          name: wf.name,
          nodes: wf.nodes,
          edges: wf.edges,
        });
        if (!cancelled) setPrepared({ name: wf.name, code: encoded });
      } catch (e) {
        if (!cancelled)
          setError(
            e instanceof Error ? e.message : "could not prepare share code",
          );
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workflowId]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setError("clipboard write was blocked -- select the code and copy it manually");
    }
  };

  const caption = `Check out "${name}" -- a workflow I built with AgentMesh`;
  const tweetUrl = `https://twitter.com/intent/tweet?text=${encodeURIComponent(caption)}`;
  const emailBody =
    code.length > 0 && code.length <= MAILTO_SAFE_CODE_LENGTH
      ? `${caption}\n\nPaste this into Import on the Workflows page:\n\n${code}`
      : `${caption}\n\nI copied the workflow code to my clipboard -- ask me for it, then paste it into Import on the Workflows page.`;
  const mailUrl = `mailto:?subject=${encodeURIComponent(caption)}&body=${encodeURIComponent(emailBody)}`;

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
        aria-label="Share workflow"
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
              Share workflow
            </h2>
            <p style={{ margin: "3px 0 0", fontSize: "var(--t-2)", color: "var(--fg-muted)" }}>
              {name || "Loading…"}
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

        {loading && (
          <div style={{ padding: "24px 0", textAlign: "center", fontSize: "var(--t-2)", color: "var(--fg-dim)" }}>
            Preparing share code…
          </div>
        )}

        {!loading && error && (
          <div style={{ fontSize: "var(--t-2)", color: "var(--danger)", padding: "8px 0" }}>
            {error}
          </div>
        )}

        {!loading && !error && code && (
          <>
            <div style={{ fontSize: "var(--t-1)", color: "var(--fg-dim)", marginBottom: 6 }}>
              Code ({formatBytes(code.length)}) -- API keys are never included, so
              re-enter them after importing.
            </div>
            <textarea
              readOnly
              value={code}
              onFocus={(e) => e.currentTarget.select()}
              style={{
                width: "100%",
                height: 88,
                resize: "none",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--t-1)",
                lineHeight: 1.5,
                padding: 10,
                background: "var(--bg-elev-2)",
                border: "1px solid var(--border)",
                borderRadius: "var(--r-2)",
                color: "var(--fg-muted)",
                marginBottom: 12,
                wordBreak: "break-all",
              }}
            />
            <button
              type="button"
              onClick={handleCopy}
              style={{
                width: "100%",
                height: 38,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                gap: "var(--s-2)",
                borderRadius: "var(--r-2)",
                border: "1px solid var(--accent-line)",
                background: "var(--accent)",
                color: "var(--accent-fg)",
                fontSize: "var(--t-3)",
                fontWeight: 600,
                cursor: "pointer",
                marginBottom: 14,
              }}
            >
              <IconCopy size={13} /> {copied ? "Copied!" : "Copy code"}
            </button>

            <div
              style={{
                height: 1,
                background: "var(--border)",
                margin: "0 0 14px",
              }}
            />

            <div style={{ fontSize: "var(--t-1)", color: "var(--fg-dim)", marginBottom: 8 }}>
              Or tell someone about it
            </div>
            <div style={{ display: "flex", gap: "var(--s-3)" }}>
              <a
                href={tweetUrl}
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  flex: 1,
                  height: 36,
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
                  textDecoration: "none",
                  cursor: "pointer",
                }}
              >
                <IconX size={12} /> Post
              </a>
              <a
                href={mailUrl}
                style={{
                  flex: 1,
                  height: 36,
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
                  textDecoration: "none",
                  cursor: "pointer",
                }}
              >
                <IconMail size={13} /> Email
              </a>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
