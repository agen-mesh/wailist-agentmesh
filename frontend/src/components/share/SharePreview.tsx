"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Logo } from "@/components/ui";
import { useAuth } from "@/hooks/useAuth";
import { useReadOnly } from "@/hooks/useReadOnly";
import { can } from "@/lib/readonly";
import { shares } from "@/lib/api";
import { SharedWorkflowCard } from "./SharedWorkflowCard";
import { shareHref, workflowHref } from "@/lib/routes";
import type { ShareImportRequirements, WorkflowShare } from "@/lib/types";

// What somebody sees when they open a link they were sent.
//
// The page is deliberately readable signed OUT -- see the route comment. The
// shape of it follows from that: show enough that a stranger can decide
// whether they want this before being asked to make an account, and be honest
// about what they will have to supply themselves, because a workflow that
// silently cannot run is worse than one that says what it needs.

export function SharePreview({
  token,
  initial,
}: {
  token: string;
  /** Read on the server by the route above, so the page has content in its
   *  HTML rather than after a round trip. Absent in mock mode, in the static
   *  mobile export, and whenever that fetch failed -- in which case the
   *  effect below does what it always did. */
  initial?: { share: WorkflowShare; requirements: ShareImportRequirements };
}) {
  const router = useRouter();
  const { signedIn, loading: authLoading } = useAuth();
  // Importing creates a workflow, so it is authoring, and this app authors on
  // a computer only -- the same policy that withholds New workflow and the
  // canvas from a phone. Preview stays open to everyone, because reading what
  // somebody sent you is not authoring and a link is mostly opened on a phone.
  //
  // Without this the page offered an Import button on a phone that could not
  // work: assertWritable rejects POST /shares/{token}/import for any handheld
  // or native client, so pressing it produced an error instead of a workflow.
  // Saying so up front is the honest version of the same rule.
  const readOnly = useReadOnly();
  const canImport = can("workflow.create", readOnly);

  const [share, setShare] = useState<WorkflowShare | null>(
    initial?.share ?? null,
  );
  const [requirements, setRequirements] =
    useState<ShareImportRequirements | null>(initial?.requirements ?? null);
  const [error, setError] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);
  const loading = !share && !error;

  useEffect(() => {
    // Already rendered from the server's copy; fetching it again would be a
    // second request for bytes that are on the page.
    if (initial) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await shares.read(token);
        if (!cancelled) {
          setShare(res.share);
          setRequirements(res.requirements);
        }
      } catch (e) {
        if (!cancelled) {
          setError(
            e instanceof Error
              ? e.message
              : "this share link is no longer available",
          );
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token, initial]);

  // Importing is always a press, never something that happens on arrival.
  //
  // An earlier version carried ?import=1 through sign-in and fired this from
  // an effect, to save the returning visitor a click. Two things were wrong
  // with that: a write triggered by navigation runs again on Back, quietly
  // giving somebody two copies of the same workflow, and it trips this repo's
  // react-hooks/set-state-in-effect rule. A signed-in visitor lands back here
  // with the button already saying "Import to my workspace", which is one
  // press and no ambiguity about when the copy was made.
  const handleImport = async () => {
    if (!signedIn) {
      // safeNextPath validates this on the other side; shareHref is already an
      // app-relative path, so it survives that check.
      router.push(`/signin?next=${encodeURIComponent(shareHref(token))}`);
      return;
    }
    setImporting(true);
    setError(null);
    try {
      const wf = await shares.importInto(token);
      router.push(workflowHref(wf.id));
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "could not import this workflow",
      );
      setImporting(false);
    }
  };

  return (
    <div
      style={{
        minHeight: "100dvh",
        background: "var(--bg)",
        color: "var(--fg)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        padding: "48px 20px 64px",
      }}
    >
      <header style={{ marginBottom: 32 }}>
        <Link
          href="/"
          aria-label="AgentMesh"
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 9,
            color: "var(--fg)",
            textDecoration: "none",
          }}
        >
          {/* Logo draws the mark AND the wordmark, which is how Topbar and the
              sign-in screen use it. This page used to add a second
              "AgentMesh" beside it, so the header read the name twice. */}
          <Logo size={20} />
        </Link>
      </header>

      <main
        className="reveal"
        style={{
          width: "100%",
          maxWidth: 560,
          border: "1px solid var(--border)",
          borderRadius: "var(--r-3)",
          background: "var(--bg-elev-1)",
          padding: 28,
        }}
      >
        {loading && (
          <p style={{ margin: 0, fontSize: 13, color: "var(--fg-muted)" }}>
            Opening this link…
          </p>
        )}

        {error && !share && (
          <>
            <h1 style={{ fontSize: 18, fontWeight: 700, margin: "0 0 8px" }}>
              This link isn&apos;t available
            </h1>
            <p
              style={{
                margin: 0,
                fontSize: 13,
                lineHeight: 1.6,
                color: "var(--fg-muted)",
                maxWidth: "60ch",
              }}
            >
              It may have been revoked by whoever shared it, or it may have
              expired. Ask them for a fresh one.
            </p>
          </>
        )}

        {share && (
          <>
            <SharedWorkflowCard
              variant="page"
              name={share.name}
              description={share.description}
              nodeCount={share.nodeCount}
              edgeCount={share.edgeCount}
              graph={share.graph}
              requirements={requirements ?? undefined}
              expiresAt={share.expiresAt}
            />

            {error && (
              <p
                style={{
                  margin: "0 0 12px",
                  fontSize: 12.5,
                  color: "var(--danger)",
                }}
              >
                {error}
              </p>
            )}

            {canImport ? (
              <>
                <button
                  type="button"
                  className="share-import-btn"
                  onClick={handleImport}
                  disabled={importing}
                >
                  {importing
                    ? "Importing…"
                    : signedIn
                      ? "Import to my workspace"
                      : "Sign in to import"}
                </button>
                {!signedIn && !authLoading && (
                  <p
                    style={{
                      margin: "10px 0 0",
                      fontSize: 11.5,
                      textAlign: "center",
                      color: "var(--fg-dim)",
                    }}
                  >
                    You&apos;ll come straight back here.
                  </p>
                )}
              </>
            ) : (
              <div
                style={{
                  border: "1px solid var(--border)",
                  borderRadius: "var(--r-2)",
                  padding: "13px 15px",
                  textAlign: "center",
                  fontSize: 12.5,
                  lineHeight: 1.6,
                  color: "var(--fg-muted)",
                }}
              >
                Open this link on a computer to import it.
                <br />
                <span style={{ color: "var(--fg-dim)", fontSize: 11.5 }}>
                  Workflows are built and imported in the desktop app.
                </span>
              </div>
            )}
          </>
        )}
      </main>

      {/* For the reader who has never heard of any of this.

          Most people who open a share link arrive with no account and no idea
          what AgentMesh is -- a friend sent them a URL. The page above talks
          entirely about one workflow and assumes the rest, which leaves a
          stranger with a card, a button, and no reason to press it. */}
      <footer
        style={{
          marginTop: 28,
          maxWidth: 560,
          width: "100%",
          textAlign: "center",
          fontSize: 12.5,
          lineHeight: 1.7,
          color: "var(--fg-dim)",
        }}
      >
        <span style={{ color: "var(--fg-muted)" }}>
          AgentMesh builds and runs AI agent workflows.
        </span>{" "}
        <Link href="/" style={{ color: "var(--accent)" }}>
          See what it does
        </Link>
      </footer>
    </div>
  );
}
