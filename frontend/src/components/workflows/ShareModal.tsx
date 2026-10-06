"use client";
import { useEffect, useRef, useState } from "react";
import { IconArrow, IconClose } from "@/components/ui";
import { Skeleton } from "@/components/ui/Skeleton";
import { useModalDismissal } from "@/hooks/useModalDismissal";
import { shares as sharesApi } from "@/lib/api";
import { shareUrl } from "@/lib/routes";
import type { ShareRedactions, WorkflowShare } from "@/lib/types";
import { encodeWorkflowShare } from "@/lib/workflowShare";

// Handing a workflow to somebody else.
//
// A link first, a code second. This dialog hands over the link and gets out of
// the way: there are no Post or Email buttons, because a link on the clipboard
// already goes wherever the person sending it wants, and a pair of buttons
// picking two destinations out of all of them is a guess the dialog does not
// need to make. An earlier version shipped them, and they could not even carry
// a code -- they sent a caption and told the recipient to ask for the
// clipboard separately, because a code is routinely longer than a mailto:
// survives.
//
// The code is still here for a channel that mangles URLs, and is built from
// the graph the SERVER returned rather than from the workflow's own nodes.
// One sanitiser, on the server; the client is never trusted to reproduce it.

const EXPIRY_CHOICES = [
  { label: "No expiry", days: 0 },
  { label: "7 days", days: 7 },
  { label: "30 days", days: 30 },
] as const;

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

const isLive = (s: WorkflowShare) =>
  !s.revokedAt &&
  (!s.expiresAt || new Date(s.expiresAt).getTime() > Date.now());

// What a link's life says about it, in the words the sharer thinks in.
//
// A revoked or expired link stays on screen rather than disappearing. The
// listing returns them deliberately, and "I revoked that one" is the answer
// to "why did my friend say the link was dead" -- which a row that quietly
// vanished cannot give.
function lifeOf(s: WorkflowShare): { label: string; dead: boolean } {
  if (s.revokedAt) return { label: "Revoked", dead: true };
  if (!s.expiresAt) return { label: "Never expires", dead: false };
  const ms = new Date(s.expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms)) return { label: "Never expires", dead: false };
  if (ms <= 0) return { label: "Expired", dead: true };
  const days = Math.ceil(ms / 86_400_000);
  if (days === 1) return { label: "Expires tomorrow", dead: false };
  return { label: `Expires in ${days} days`, dead: false };
}

function importsOf(s: WorkflowShare): string {
  const n = s.importCount ?? 0;
  if (n === 0) return "no imports yet";
  return n === 1 ? "1 import" : `${n} imports`;
}

// Sentences, not a count table. "your API key" is what a person needs to hear;
// "apiKeys: 1" is what the API happens to return.
function redactionLines(r: ShareRedactions): string[] {
  const lines: string[] = [];
  const plural = (n: number, one: string, many: string) =>
    n === 1 ? one : `${n} ${many}`;
  if (r.apiKeys > 0) lines.push(plural(r.apiKeys, "your API key", "API keys"));
  if (r.secrets > 0)
    lines.push(plural(r.secrets, "a connector secret", "connector secrets"));
  if (r.webhookSecrets > 0) lines.push("your webhook secret");
  if (r.connectedAccounts > 0) lines.push("your connected accounts");
  if (r.uploadedFiles > 0)
    lines.push(plural(r.uploadedFiles, "an uploaded file", "uploaded files"));
  if (r.agentWallets > 0) lines.push("your agent wallet addresses");
  if (r.emailAddresses > 0) lines.push("the addresses it sends email to");
  if (r.leasedMachines > 0) lines.push("your leased machine");
  return lines;
}

// Mounted only while open (the parent renders it conditionally on
// shareWorkflowId, the same pattern AddToWorkflowDialog uses) -- so a fresh
// mount per share is the reset, and no effect has to clear anything.
export function ShareModal({
  workflowId,
  onClose,
}: {
  workflowId: string;
  onClose: () => void;
}) {
  const [share, setShare] = useState<WorkflowShare | null>(null);
  const [redactions, setRedactions] = useState<ShareRedactions | null>(null);
  const [others, setOthers] = useState<WorkflowShare[]>([]);
  const [expiryDays, setExpiryDays] = useState<number>(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<"link" | "code" | null>(null);
  // The token whose Revoke button is currently asking "Revoke?". One at a
  // time: arming a second row disarms the first, which is what a person
  // expects and saves a per-row state.
  const [confirming, setConfirming] = useState<string | null>(null);
  // Everything beyond the link on screen is behind one line until asked for.
  const [optionsOpen, setOptionsOpen] = useState(false);
  const loading = !share && !error;

  // "No expiry" is offered only when the current link DOES expire.
  //
  // It used to be the default, so the press next to it minted a second
  // never-expiring link to the same workflow -- a duplicate of the one in the
  // panel, against an allowance counted per user, for no benefit. Taking the
  // option away where it would do nothing is better than disabling the button
  // and explaining why: there is then no state in which this control makes
  // something pointless.
  const expiryChoices = EXPIRY_CHOICES.filter(
    (c) => c.days !== 0 || !!share?.expiresAt,
  );
  // Derived rather than synced in an effect -- `share` arrives after the
  // first render, so a state this depends on would need an effect to correct
  // it, which is both the slower path and the one this repo's
  // set-state-in-effect rule exists to stop.
  const expirySelection = expiryChoices.some((c) => c.days === expiryDays)
    ? expiryDays
    : (expiryChoices[0]?.days ?? 0);

  const dialogRef = useModalDismissal(onClose);

  // One call, and the server decides whether this is a new link or one the
  // workflow already has. Opening the dialog four times should not leave four
  // links behind for somebody to wonder about later.
  //
  // This was list-then-maybe-reuse, decided here, and it was wrong twice over.
  // It reused the newest live link without asking whether the workflow had
  // changed since, so editing and reopening handed back a link to the
  // PREVIOUS version with nothing on screen saying so. And that branch had no
  // redaction counts to set, so the "not included ..." summary disappeared at
  // exactly the moment somebody was about to hand the link over.
  //
  // Both follow from the same mistake: only the server holds the sanitised
  // graph, so only the server can tell whether a link still describes this
  // workflow. Asked properly, it answers with the counts either way.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const created = await sharesApi.create(workflowId, 0, true);
        if (cancelled) return;
        setShare(created.share);
        setRedactions(created.redactions);
        // The listing feeds the revoke list below and nothing else, so it is
        // fetched after, with the link just obtained filtered out of it.
        const existing = await sharesApi.listFor(workflowId);
        if (cancelled) return;
        setOthers(existing.filter((s) => s.token !== created.share.token));
      } catch (e) {
        if (!cancelled) {
          setError(
            e instanceof Error ? e.message : "could not prepare a share link",
          );
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workflowId]);

  // Cleared on unmount. The dialog can be closed inside the 1.5s window --
  // copying a link and immediately pressing Escape is the normal way to use
  // this -- and the timer would then fire against a component that is gone.
  const flashTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (flashTimer.current !== null) clearTimeout(flashTimer.current);
    },
    [],
  );

  const flash = (what: "link" | "code") => {
    setCopied(what);
    if (flashTimer.current !== null) clearTimeout(flashTimer.current);
    flashTimer.current = setTimeout(() => setCopied(null), 1500);
  };

  const handleCopyLink = async () => {
    if (!share) return;
    try {
      await navigator.clipboard.writeText(shareUrl(share.token));
      flash("link");
    } catch {
      // Deliberately nothing. The link is on screen, in a field that selects
      // itself when focused -- that IS the fallback, and raising an error for
      // something the person can still do by hand is noise. CopyField on the
      // Helixbox console makes the same call for the same reason. A code is
      // different: that value is not on screen, so its failure still speaks.
    }
  };

  // The graph is fetched here rather than kept around: the listing deliberately
  // does not carry graphs, and most people copy the link and never touch this.
  const handleCopyCode = async () => {
    if (!share || busy) return;
    setBusy(true);
    setError(null);
    try {
      const { share: full } = await sharesApi.read(share.token);
      const code = await encodeWorkflowShare({
        name: full.name,
        description: full.description,
        nodes: full.graph.nodes,
        edges: full.graph.edges,
      });
      await navigator.clipboard.writeText(code);
      flash("code");
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not prepare a code");
    } finally {
      setBusy(false);
    }
  };

  const handleNewLink = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const created = await sharesApi.create(workflowId, expirySelection);
      setOthers((prev) => (share ? [share, ...prev] : prev));
      setShare(created.share);
      setRedactions(created.redactions);
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not create a link");
    } finally {
      setBusy(false);
    }
  };

  // Revoking asks first, inline, by turning the button into "Revoke?".
  //
  // It is instant and irreversible, and the link may already be in somebody
  // else's inbox -- so a stray click on a 26px button should not be the whole
  // interaction. Inline rather than a nested confirm dialog: a second modal
  // over this one is heavier than the decision warrants, and the row itself
  // is the thing being talked about.
  const handleRevoke = async (token: string) => {
    if (confirming !== token) {
      setConfirming(token);
      return;
    }
    setConfirming(null);
    setError(null);
    try {
      await sharesApi.revoke(token);
      // Marked revoked in place rather than dropped, so the sharer can see
      // what they just did. lifeOf() greys it and the Revoke button goes.
      const stamp = new Date().toISOString();
      setOthers((prev) =>
        prev.map((s) => (s.token === token ? { ...s, revokedAt: stamp } : s)),
      );
      setShare((cur) =>
        cur && cur.token === token ? { ...cur, revokedAt: stamp } : cur,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not revoke that link");
    }
  };

  const url = share ? shareUrl(share.token) : "";

  const lines = redactions ? redactionLines(redactions) : [];
  const currentLife = share
    ? lifeOf(share)
    : { label: "", dead: false as boolean };
  // Every other link, dead ones included. They used to be filtered out, which
  // threw away the only record of what had been handed out and then pulled
  // back -- and the backend returns them precisely so this view can show them.
  const otherLinks = others;

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
        aria-label="Share workflow"
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
              Share workflow
            </h2>
            <p
              style={{
                margin: "3px 0 0",
                fontSize: "var(--t-2)",
                color: "var(--fg-muted)",
              }}
            >
              {share?.name || "Loading…"}
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

        {/* "Copied!" is a label swap on a button, which a screen reader does
            not announce -- so the one piece of feedback the whole dialog
            gives was silent. This says it out loud without showing twice. */}
        <span
          aria-live="polite"
          style={{
            position: "absolute",
            width: 1,
            height: 1,
            overflow: "hidden",
            clip: "rect(0 0 0 0)",
            whiteSpace: "nowrap",
          }}
        >
          {loading
            ? "Preparing a share link"
            : copied === "link"
              ? "Link copied to clipboard"
              : copied === "code"
                ? "Code copied to clipboard"
                : ""}
        </span>

        {/* Shaped like the bands that are coming, rather than a line of
            centred text, for the reason ui/Skeleton.tsx sets out: a line of
            text occupies almost none of the room the real thing needs, so the
            panel snapped to a new height the moment the link arrived --
            under a cursor already on its way to the Copy button.

            aria-hidden with the live region above doing the talking, which is
            how WorkflowListSkeleton handles the same problem: a screen reader
            hears "preparing a share link" once instead of eight rectangles. */}
        {loading && (
          <div aria-hidden="true">
            <div
              style={{ display: "flex", gap: "var(--s-3)", marginBottom: 8 }}
            >
              <Skeleton height={38} radius="var(--r-2)" style={{ flex: 1 }} />
              <Skeleton width={104} height={38} radius="var(--r-2)" />
            </div>
            <Skeleton width="72%" height={11} style={{ marginBottom: 10 }} />
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: "var(--s-3)",
                marginBottom: 14,
              }}
            >
              <Skeleton width="40%" height={11} />
              <Skeleton width={62} height={26} radius="var(--r-2)" />
            </div>
            <div
              style={{ display: "flex", gap: "var(--s-3)", marginBottom: 9 }}
            >
              <Skeleton height={36} radius="var(--r-2)" style={{ flex: 1 }} />
              <Skeleton height={36} radius="var(--r-2)" style={{ flex: 1 }} />
            </div>
            <Skeleton width="88%" height={11} style={{ marginBottom: 7 }} />
            <Skeleton width="46%" height={11} style={{ marginBottom: 37 }} />
            <Skeleton width="92%" height={11} style={{ marginBottom: 8 }} />
            <Skeleton width="58%" height={11} style={{ marginBottom: 22 }} />
            <Skeleton width="28%" height={11} style={{ marginBottom: 11 }} />
          </div>
        )}

        {/* Only the failure that leaves nothing to show. Everything else
            this dialog can fail at happens next to a control further down,
            and is reported there -- see below. SharePreview splits its two
            the same way. */}
        {error && !share && (
          <div
            style={{
              fontSize: "var(--t-2)",
              color: "var(--danger)",
              padding: "8px 0",
              maxWidth: "60ch",
            }}
          >
            {error}
          </div>
        )}

        {share && (
          <>
            {/* Band 1 -- the thing people opened this dialog for.
                It used to be fifth down the panel, below a disclosure line, a
                full-width field nobody reads and a status row. Field and
                button side by side is how CopyField does it on the Helixbox
                console; the difference here is that copying is this dialog's
                primary action, so it keeps the accent rather than going
                ghost. */}
            <div
              style={{ display: "flex", gap: "var(--s-3)", marginBottom: 8 }}
            >
              <input
                readOnly
                value={url}
                onFocus={(e) => e.currentTarget.select()}
                aria-label="Share link"
                className="share-linkfield"
                style={{
                  flex: 1,
                  minWidth: 0,
                  height: 38,
                  fontFamily: "var(--font-mono)",
                  fontSize: "var(--t-1)",
                  padding: "0 10px",
                  background: "var(--bg-elev-2)",
                  border: "1px solid var(--border)",
                  borderRadius: "var(--r-2)",
                  color: "var(--fg-muted)",
                }}
              />
              <button
                type="button"
                onClick={handleCopyLink}
                disabled={currentLife.dead}
                className="share-primary-btn"
                style={{ width: "auto", flex: "0 0 auto", padding: "0 14px" }}
              >
                <IconCopy size={13} />{" "}
                {currentLife.dead
                  ? "Link is dead"
                  : copied === "link"
                    ? "Copied"
                    : "Copy link"}
              </button>
            </div>

            {/* Band 2 -- what this link lets somebody do, and what it has
                done. The second line was previously shown for every OTHER
                link but never for the one on screen, so the most interesting
                question a sharer has -- has anyone actually used it? -- was
                the one the dialog would not answer. */}
            <p
              style={{
                margin: "0 0 4px",
                fontSize: "var(--t-1)",
                lineHeight: 1.5,
                color: "var(--fg-dim)",
              }}
            >
              Anyone with this link can open the workflow and import a copy.
            </p>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: "var(--s-3)",
                minHeight: 26,
                marginBottom: 14,
                fontSize: "var(--t-1)",
                color: currentLife.dead ? "var(--warm)" : "var(--fg-dim)",
              }}
            >
              <span style={{ fontVariantNumeric: "tabular-nums" }}>
                {currentLife.label} · {importsOf(share)}
              </span>
              {isLive(share) && (
                <button
                  type="button"
                  onClick={() => void handleRevoke(share.token)}
                  className="share-revoke-btn"
                >
                  {confirming === share.token ? "Revoke?" : "Revoke"}
                </button>
              )}
            </div>

            {/* Band 3 -- the other way to hand it over.
                A code used to be a third ghost button beside two that posted
                the link to X and opened a mail client, the same size and
                shape as them, and the one thing it does differently -- it
                cannot be taken back -- was said AFTERWARDS, in a warning that
                appeared once the copying was already done. A warning read
                after the act is the weaker half of the same sentence, so it
                is now the sentence. */}
            <p
              style={{
                margin: 0,
                fontSize: "var(--t-1)",
                lineHeight: 1.6,
                color: "var(--fg-dim)",
                maxWidth: "60ch",
              }}
            >
              or{" "}
              <button
                type="button"
                onClick={handleCopyCode}
                disabled={busy || currentLife.dead}
                className="share-textlink"
              >
                {copied === "code"
                  ? "code copied"
                  : busy
                    ? "preparing…"
                    : "copy a code"}
              </button>{" "}
              instead — it can&apos;t be revoked, but it works where a link
              won&apos;t.
            </p>

            {/* Revoking, preparing a code and minting a link all fail
                here rather than at the top of the panel. The panel is ~300px
                tall; a message about the button you just pressed, printed
                above the link field, is a message you do not see. */}
            {error && (
              <p
                style={{
                  margin: "10px 0 0",
                  fontSize: "var(--t-2)",
                  lineHeight: 1.5,
                  color: "var(--danger)",
                  maxWidth: "60ch",
                }}
              >
                {error}
              </p>
            )}

            <div
              style={{
                height: 1,
                background: "var(--border)",
                margin: "14px 0 12px",
              }}
            />

            {/* Band 4 -- what the recipient does NOT get.
                The same words it has always had, minus the border and the
                fill. A bordered, filled card was the fourth box in a panel
                that had four, and none of what this says needed one. */}
            <p
              style={{
                margin: 0,
                fontSize: "var(--t-2)",
                lineHeight: 1.65,
                color: "var(--fg-muted)",
                maxWidth: "60ch",
              }}
            >
              <strong style={{ color: "var(--fg)", fontWeight: 600 }}>
                Not included:
              </strong>{" "}
              {lines.length > 0
                ? `${lines.join(", ")}. Whoever imports it adds their own.`
                : "any API keys, secrets or uploaded files. Whoever imports it adds their own."}
            </p>

            <div
              style={{
                height: 1,
                background: "var(--border)",
                margin: "14px 0",
              }}
            />

            {/* Band 5 -- everything about this workflow's links beyond the
                one in the panel: making another with an expiry, and what has
                already been handed out.

                Collapsed until asked for, for the reason the Credits screen
                gives for doing the same with "How credits work": it costs one
                line rather than a field. Both of these were permanently on
                screen, the second of them a list that grows without bound
                inside a fixed panel, and between them they were two of the
                four things competing with the Copy button. Nothing is
                removed; the label carries the count so the line is worth
                reading while shut. */}
            <button
              type="button"
              className="share-disclosure"
              aria-expanded={optionsOpen}
              onClick={() => setOptionsOpen((v) => !v)}
            >
              <span>
                Link options
                {otherLinks.length > 0 &&
                  ` · ${otherLinks.length} other${otherLinks.length === 1 ? "" : "s"}`}
              </span>
              <span
                className="share-disclosure__chevron"
                data-open={optionsOpen}
                aria-hidden
              >
                <IconArrow size={12} />
              </span>
            </button>

            {optionsOpen && (
              <div style={{ paddingTop: 10 }}>
                <div
                  style={{
                    marginBottom: otherLinks.length > 0 ? 14 : 0,
                  }}
                >
                  {/* Worded so it cannot be read as applying to the link
                      above. It sets the expiry of the link the button beside
                      it MAKES -- labelled "New link expires", people
                      reasonably read it as putting an expiry on the one they
                      had just copied. */}
                  <span
                    id="share-expiry-label"
                    style={{
                      display: "block",
                      marginBottom: 6,
                      fontSize: "var(--t-1)",
                      color: "var(--fg-dim)",
                    }}
                  >
                    Make another, expiring in
                  </span>
                  {/* The label sits on its own line because three segments and
                      Make it do not fit beside it in a 480px panel. */}
                  {/* stretch, not center: on a touch pointer the 44px floor
                      in globals.css applies to each SEGMENT, which makes the
                      track taller than this button's own 36px. Letting the
                      button take its height from the row keeps the two edges
                      aligned at either size instead of centring a short
                      button against a tall track. */}
                  <div
                    style={{
                      display: "flex",
                      alignItems: "stretch",
                      gap: "var(--s-3)",
                    }}
                  >
                    <div
                      className="share-seg"
                      role="radiogroup"
                      aria-labelledby="share-expiry-label"
                    >
                      {expiryChoices.map((c) => (
                        <button
                          key={c.days}
                          type="button"
                          role="radio"
                          aria-checked={c.days === expirySelection}
                          className="share-seg__item"
                          onClick={() => setExpiryDays(c.days)}
                        >
                          {c.label}
                        </button>
                      ))}
                    </div>
                    <button
                      type="button"
                      onClick={handleNewLink}
                      disabled={busy}
                      className="share-ghost-btn"
                      style={{
                        flex: "0 0 auto",
                        padding: "0 12px",
                        height: "auto",
                      }}
                    >
                      Make it
                    </button>
                  </div>
                </div>

                {otherLinks.length > 0 && (
                  <div>
                    <div
                      style={{
                        fontSize: "var(--t-1)",
                        color: "var(--fg-dim)",
                        marginBottom: 6,
                      }}
                    >
                      Other links to this workflow
                    </div>
                    {otherLinks.map((s) => (
                      <div
                        key={s.token}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "space-between",
                          gap: "var(--s-3)",
                          padding: "6px 0",
                          fontSize: "var(--t-1)",
                          color: "var(--fg-muted)",
                        }}
                      >
                        <span
                          style={{
                            fontFamily: "var(--font-mono)",
                            fontVariantNumeric: "tabular-nums",
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                            // A dead link is kept, and shown as spent rather
                            // than as an option.
                            opacity: lifeOf(s).dead ? 0.55 : 1,
                            textDecoration: lifeOf(s).dead
                              ? "line-through"
                              : undefined,
                          }}
                        >
                          …{s.token.slice(-8)} · {importsOf(s)} ·{" "}
                          {lifeOf(s).label}
                        </span>
                        {isLive(s) && (
                          <button
                            type="button"
                            onClick={() => void handleRevoke(s.token)}
                            className="share-revoke-btn"
                          >
                            {confirming === s.token ? "Revoke?" : "Revoke"}
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
