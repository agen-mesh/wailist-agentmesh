"use client";
import { useState } from "react";
import { IconClose } from "@/components/ui";
import { useModalDismissal } from "@/hooks/useModalDismissal";
import { shares as sharesApi, workflows as workflowsApi } from "@/lib/api";
import { classifyShareInput } from "@/lib/shareInput";
import type { ShareImportRequirements, WorkflowShare } from "@/lib/types";
import { SharedWorkflowCard } from "@/components/share/SharedWorkflowCard";
import type { WorkflowShareData } from "@/lib/workflowShare";
import { decodeWorkflowShare } from "@/lib/workflowShare";

// One box, three kinds of paste: a link, a bare token, or a code.
//
// Which one it is gets worked out in lib/shareInput.ts rather than here, and
// the person pasting is never asked to say. They were handed one string by a
// friend; being made to classify it first is exactly the friction that leaves
// a feature unused.

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

// Mounted only while open (the parent renders it conditionally on importOpen,
// the same pattern AddToWorkflowDialog uses) -- so each open is a fresh mount
// and a fresh slate, with no reset-on-open effect needed.
// What the paste turned out to be, once it has been looked at but before
// anything has been created.
//
// A link resolves against the backend, which answers with the same snapshot
// and requirements the public /s/ page shows. A code is decoded locally and
// has no requirements: those are derived server-side from a stored snapshot,
// and a code has no row anywhere until it is imported.
type Resolved =
  | {
      kind: "link";
      token: string;
      share: WorkflowShare;
      req: ShareImportRequirements;
    }
  | { kind: "code"; data: WorkflowShareData };

export function ImportModal({
  onClose,
  onImported,
}: {
  onClose: () => void;
  onImported: (id: string) => void;
}) {
  const [input, setInput] = useState("");
  const [resolving, setResolving] = useState(false);
  const [resolved, setResolved] = useState<Resolved | null>(null);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const dialogRef = useModalDismissal(onClose);

  const handlePasteFromClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      setInput(text);
      setError(null);
    } catch {
      setError(
        "clipboard read was blocked -- paste manually with Ctrl+V (Cmd+V on Mac)",
      );
    }
  };

  // A code goes through POST /workflows/import, not create-then-update. That
  // endpoint is atomic and runs the graph through the server's sanitiser --
  // the ordinary save path deliberately passes an "enc:"-prefixed value
  // through untouched, which is right for the canvas round-tripping its own
  // ciphertext and wrong for a stranger's paste.
  // Look at the paste; create nothing.
  //
  // Importing used to happen on the first press, so a pasted link produced a
  // workflow sight unseen -- while the very same link opened in a browser
  // showed a page saying what it was and what it would need. Same link, two
  // completely different amounts of respect for the person holding it.
  const resolve = async () => {
    if (!input.trim() || resolving) return;
    setResolving(true);
    setError(null);

    const parsed = classifyShareInput(input);
    if (!parsed) {
      setError("paste a link or a code first");
      setResolving(false);
      return;
    }

    try {
      if (parsed.kind === "code") {
        setResolved({
          kind: "code",
          data: await decodeWorkflowShare(parsed.code),
        });
        return;
      }
      try {
        const res = await sharesApi.read(parsed.token);
        setResolved({
          kind: "link",
          token: parsed.token,
          share: res.share,
          req: res.requirements,
        });
      } catch (tokenErr) {
        // A bare base64url string is shaped like a token AND like a short
        // legacy code -- see classifyShareInput. The lookup failing is what
        // settles it, so the paste gets its second reading rather than an
        // error about a link that was never a link. If the code reading fails
        // too, the token error is the honest one to show: it is far and away
        // the likelier thing somebody pasted.
        if (input.trim() !== parsed.token) throw tokenErr;
        try {
          setResolved({
            kind: "code",
            data: await decodeWorkflowShare(parsed.token),
          });
        } catch {
          throw tokenErr;
        }
      }
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "could not read that -- check the link or code and try again",
      );
    } finally {
      setResolving(false);
    }
  };

  const confirmImport = async () => {
    if (!resolved || importing) return;
    setImporting(true);
    setError(null);
    try {
      if (resolved.kind === "link") {
        const wf = await sharesApi.importInto(resolved.token);
        onImported(wf.id);
        return;
      }
      const { data } = resolved;
      const wf = await workflowsApi.importGraph({
        name: data.name?.trim() || "Imported workflow",
        description: data.description?.trim() || undefined,
        nodes: data.nodes,
        edges: data.edges,
      });
      onImported(wf.id);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "could not import this workflow",
      );
      setImporting(false);
    }
  };

  return (
    <div
      role="presentation"
      className="share-scrim"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Import workflow"
        className="share-panel"
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
            <p
              style={{
                margin: "3px 0 0",
                fontSize: "var(--t-2)",
                color: "var(--fg-muted)",
                maxWidth: "60ch",
              }}
            >
              {resolved
                ? "Check this before importing"
                : "Paste a share link or a code"}
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="share-icon-btn"
          >
            <IconClose size={13} />
          </button>
        </div>

        {resolved ? (
          <SharedWorkflowCard
            variant="dialog"
            className="reveal"
            {...cardPropsFor(resolved)}
          />
        ) : (
          // Two rows, not five, and 13px rather than 11.
          //
          // What arrives here is one line out of a chat message. A tall
          // monospace box at the smallest type size in the app read as "paste
          // a config blob", which is both the wrong size for the job and the
          // wrong promise about what the dialog wants. It stays monospace --
          // a token is a machine value and this app sets those in mono --
          // and it stays a textarea, because a code that arrived wrapped is
          // pasted as several lines.
          <div style={{ position: "relative", marginBottom: 12 }}>
            <textarea
              autoFocus
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                setError(null);
              }}
              // Enter has to stay a newline -- a code that arrived wrapped is
              // pasted as several lines, and swallowing Enter would make
              // fixing one by hand impossible. Ctrl/Cmd+Enter is the usual
              // way out of that, and without it the only route forward was
              // the mouse.
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  void resolve();
                }
              }}
              rows={2}
              placeholder="https://www.agent-mesh.app/s/… or am1.…"
              className="share-linkfield"
              style={{
                display: "block",
                width: "100%",
                resize: "none",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--t-3)",
                lineHeight: 1.5,
                padding: 10,
                // Room for the Paste button while there is nothing to
                // collide with, and the full width once there is.
                paddingRight: input ? 10 : 92,
                background: "var(--bg-elev-2)",
                border: "1px solid var(--border)",
                borderRadius: "var(--r-2)",
                color: "var(--fg)",
                wordBreak: "break-all",
              }}
            />
            {/* Inside the field, and only while it is empty.
                It cost a full-width 36px row of its own to duplicate Ctrl+V,
                and once something has been pasted there is nothing left for
                it to do -- so it goes, which is also what keeps it from ever
                sitting on top of the text. */}
            {!input && (
              <button
                type="button"
                onClick={handlePasteFromClipboard}
                className="share-ghost-btn"
                style={{
                  position: "absolute",
                  top: 7,
                  right: 7,
                  flex: "0 0 auto",
                  height: 26,
                  padding: "0 9px",
                  fontSize: "var(--t-1)",
                  background: "var(--bg-elev-1)",
                }}
              >
                <IconPaste /> Paste
              </button>
            )}
          </div>
        )}

        {error && (
          <div
            style={{
              fontSize: "var(--t-2)",
              color: "var(--danger)",
              marginBottom: 12,
              maxWidth: "60ch",
            }}
          >
            {error}
          </div>
        )}

        <div style={{ display: "flex", gap: "var(--s-3)" }}>
          <button
            type="button"
            onClick={() => {
              // Back to the paste box rather than out of the dialog: having
              // looked at one link, the likeliest next move is trying a
              // different one.
              if (resolved) {
                setResolved(null);
                setError(null);
                return;
              }
              onClose();
            }}
            className="share-ghost-btn"
          >
            {resolved ? "Back" : "Cancel"}
          </button>
          <button
            type="button"
            disabled={
              resolved ? importing : !input.trim() || resolving || importing
            }
            onClick={resolved ? confirmImport : resolve}
            className="share-primary-btn"
            style={{ flex: 1, width: "auto" }}
          >
            {resolved
              ? importing
                ? "Importing…"
                : "Import to my workspace"
              : resolving
                ? "Looking…"
                : "Continue"}
          </button>
        </div>
      </div>
    </div>
  );
}

// What the paste turned out to be, in the same card the public /s/ page uses
// -- so the two ways into one workflow cannot end up describing it
// differently. The dialog used to render a flatter copy of that layout, which
// is exactly how the wording of the two drifts without anybody noticing.
//
// `requirements` is left undefined for a code, not set to an empty object:
// they are derived server-side from a stored snapshot and a code has no row
// anywhere until it is imported, and that distinction is what makes the card
// say "what it may still need" rather than claim the thing is ready to run.
function cardPropsFor(resolved: Resolved) {
  if (resolved.kind === "link") {
    const { share, req } = resolved;
    return {
      name: share.name,
      description: share.description,
      nodeCount: share.nodeCount,
      edgeCount: share.edgeCount,
      graph: share.graph,
      requirements: req,
      expiresAt: share.expiresAt,
    };
  }
  const { data } = resolved;
  return {
    name: data.name?.trim() || "Untitled workflow",
    description: data.description,
    nodeCount: data.nodes.length,
    edgeCount: data.edges.length,
    graph: { nodes: data.nodes, edges: data.edges },
    requirements: undefined,
    expiresAt: undefined,
  };
}
