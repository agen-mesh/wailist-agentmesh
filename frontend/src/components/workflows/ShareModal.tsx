"use client";
import { useEffect, useState } from "react";
import { IconClose } from "@/components/ui";
import { useModalDismissal } from "@/hooks/useModalDismissal";
import { workflows as workflowsApi } from "@/lib/api";
import { sealShare } from "@/lib/shareCrypto";
import {
  maxShareBytes,
  prepareSharePayload,
  ShareTooLargeError,
  shareURL,
} from "@/lib/sharePayload";
import { createShare, shareOrigin } from "@/lib/shares";

const authorship =
  "Signed by AgentMesh. This link does not prove which person shared it.";
const death =
  "This link dies 7 days after it is confirmed. AgentMesh does not keep a copy. A retired Arkiv testnet kills the link too.";
const privateCopy =
  "Private. The decryption key stays in the link after # and is not sent to AgentMesh or Arkiv.";
const publicCopy =
  "Public. Anyone who can query Arkiv can read this graph for 7 days, even without the link. Prompts are included.";
const waiting = "Waiting for Arkiv to confirm the share.";
const unconfirmed = "Share did not confirm. Nothing was copied. Try again.";
const shareRate =
  "You have reached the hourly share limit. Nothing was copied. Try again later.";
const shareBudget =
  "AgentMesh has reached today's share limit. Nothing was copied. Try again tomorrow.";
const shareDisabled = "Sharing is off on this server. Nothing was copied.";
const shareBusy = "Another share is being confirmed. Nothing was copied. Try again.";
const tooLarge = new ShareTooLargeError().message;

function shareFailure(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "share_rate") return shareRate;
  if (code === "share_budget") return shareBudget;
  if (code === "share_disabled") return shareDisabled;
  if (code === "share_busy") return shareBusy;
  if (code === "share_too_large") return tooLarge;
  if (code === "Workflows can only be edited in the AgentMesh desktop app.") return code;
  return unconfirmed;
}
const entityKeyPattern = /^0x[0-9a-fA-F]{64}$/;

const IconCopy = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
    <path d="M2.5 10.5v-7A1 1 0 0 1 3.5 2.5h7" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
  </svg>
);

const IconX = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="currentColor">
    <path d="M9.3 6.9 14 2h-1.6L8.6 5.9 5.4 2H1l5 6.7L1 14h1.6l4-4.3L9.9 14H14L9.3 6.9Zm-1.4 1.6-.5-.6L3.2 3h1.4l3 4 .5.6 3.9 5.3H10.6l-2.7-3.4Z" />
  </svg>
);

const IconMail = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect x="1.5" y="3.5" width="13" height="9" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
    <path d="M2 4.5 8 9l6-4.5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

export function ShareModal({
  workflowId,
  onClose,
}: {
  workflowId: string;
  onClose: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [bytes, setBytes] = useState<Uint8Array | null>(null);
  const [ready, setReady] = useState(false);
  const [tooBig, setTooBig] = useState(false);
  const [isPublic, setIsPublic] = useState(false);
  const [pending, setPending] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useModalDismissal(onClose);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const wf = await workflowsApi.get(workflowId);
        const prepared = await prepareSharePayload({
          name: wf.name,
          nodes: wf.nodes,
          edges: wf.edges,
        });
        if (cancelled) return;
        setName(wf.name);
        setBytes(prepared.bytes);
      } catch (e) {
        if (cancelled) return;
        const message = e instanceof Error ? e.message : "could not prepare share";
        setError(message);
        if (message === tooLarge) setTooBig(true);
      } finally {
        if (!cancelled) setReady(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workflowId]);

  const handleCreate = async () => {
    if (!bytes || pending || tooBig || link) return;
    setError(null);
    setPending(true);
    try {
      let payload = bytes;
      let fragment: string | null = null;
      if (!isPublic) {
        const sealed = await sealShare(bytes);
        if (sealed.ciphertext.byteLength > maxShareBytes) {
          setError(tooLarge);
          setTooBig(true);
          return;
        }
        payload = sealed.ciphertext;
        fragment = sealed.key;
      }
      const receipt = await createShare(payload);
      if (receipt.chainId !== 7794095 || !entityKeyPattern.test(receipt.entityKey)) {
        setError(unconfirmed);
        return;
      }
      setLink(shareURL(shareOrigin(), receipt.entityKey, fragment));
    } catch (err) {
      const message = shareFailure(err);
      setError(message);
      if (message === tooLarge) setTooBig(true);
    } finally {
      setPending(false);
    }
  };

  const handleCopy = async () => {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setError("clipboard write was blocked -- select the link and copy it manually");
    }
  };

  const caption = `Check out "${name}" -- a workflow I built with AgentMesh`;
  const tweetUrl = link
    ? `https://twitter.com/intent/tweet?text=${encodeURIComponent(`${caption} ${link}`)}`
    : "";
  const mailUrl = link
    ? `mailto:?subject=${encodeURIComponent(caption)}&body=${encodeURIComponent(`${caption}\n\n${link}`)}`
    : "";

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

        <p style={{ margin: "0 0 8px", fontSize: "var(--t-2)", color: "var(--fg-muted)" }}>{authorship}</p>
        <p style={{ margin: "0 0 14px", fontSize: "var(--t-2)", color: "var(--fg-muted)" }}>{death}</p>

        {bytes && (
          <label style={{ display: "flex", gap: 8, alignItems: "flex-start", marginBottom: 10, fontSize: "var(--t-2)" }}>
            <input
              type="checkbox"
              checked={isPublic}
              onChange={(e) => setIsPublic(e.target.checked)}
              disabled={pending || Boolean(link)}
            />
            Anyone with the link can read this
          </label>
        )}

        {bytes && (
          <p style={{ margin: "0 0 14px", fontSize: "var(--t-2)", color: "var(--fg-muted)" }}>
            {isPublic ? publicCopy : privateCopy}
          </p>
        )}

        {pending && (
          <p style={{ margin: "0 0 14px", fontSize: "var(--t-2)", color: "var(--fg-dim)" }}>{waiting}</p>
        )}

        {error && (
          <div style={{ fontSize: "var(--t-2)", color: "var(--danger)", padding: "0 0 12px" }}>{error}</div>
        )}

        {ready && (
          <button
            type="button"
            onClick={handleCreate}
            disabled={!bytes || tooBig || pending || Boolean(link)}
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
              cursor: !bytes || tooBig || pending || link ? "default" : "pointer",
              marginBottom: 14,
              opacity: !bytes || tooBig || pending || link ? 0.6 : 1,
            }}
          >
            Create link
          </button>
        )}

        {link && (
          <>
            <p
              style={{
                margin: "0 0 12px",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--t-1)",
                lineHeight: 1.5,
                wordBreak: "break-all",
                color: "var(--fg)",
              }}
            >
              {link}
            </p>
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
              <IconCopy size={13} /> {copied ? "Copied!" : "Copy link"}
            </button>
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
