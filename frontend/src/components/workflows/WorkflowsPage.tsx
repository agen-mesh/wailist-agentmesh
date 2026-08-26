"use client";
import { useState, useMemo, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import {
  Pill,
  Tag,
  IconSearch,
  IconGrid,
  IconWallet,
  Card,
  ghostBtnSm,
} from "@/components/ui";
import { Topbar } from "@/components/Topbar";
import { Workflow } from "@/lib/types";
import { workflows as workflowsApi } from "@/lib/api";
import { useCredits } from "@/lib/credits/store";
import { tendril } from "@/lib/tendril";
import { DEMO_WORKFLOW } from "@/lib/data";

export function WorkflowsPage() {
  const router = useRouter();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("all");
  const [view, setView] = useState<"rows" | "grid">("rows");
  const [wfList, setWfList] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [creatingTendril, setCreatingTendril] = useState(false);
  const [creatingDemo, setCreatingDemo] = useState(false);
  // Tagged by source so the banner always shows the most recent failure --
  // two separate error strings with a fixed `a || b` precedence would let
  // a stale error from one action permanently mask a newer one from the
  // other. A success only clears the error if it's the one that owns it,
  // so it never wipes an unrelated action's still-relevant error.
  const [pageError, setPageError] = useState<{
    source: "demo" | "delete";
    message: string;
  } | null>(null);
  const { balanceUSD, balanceKnown, refreshBalance } = useCredits();

  useEffect(() => {
    workflowsApi
      .list()
      .then(setWfList)
      .catch(() => setWfList([]))
      .finally(() => setLoading(false));
  }, []);

  // Same authoritative balance the engine spends against, re-read on mount so
  // this page never shows a figure left over from before the last run.
  useEffect(() => {
    void refreshBalance();
  }, [refreshBalance]);

  const filtered = useMemo(() => {
    return wfList.filter((wf) => {
      const matchesQ =
        !q ||
        wf.name?.toLowerCase().includes(q.toLowerCase()) ||
        wf.tags?.join(" ").includes(q.toLowerCase());
      const matchesS = status === "all" || wf.status === status;
      return matchesQ && matchesS;
    });
  }, [wfList, q, status]);

  const handleNewWorkflow = useCallback(async () => {
    if (creating) return;
    setCreating(true);
    try {
      const wf = await workflowsApi.create("Untitled workflow");
      router.push(`/workflows/${wf.id}`);
    } catch {
      setCreating(false);
    }
  }, [creating, router]);

  // No node graph here at all — this row is a shortcut into the direct
  // Tendril console (WorkflowRoute matches on its id), not a workflow you
  // build on canvas. tendril.console() finds-or-creates the ONE hidden
  // workflow that backs every user's console, so repeated clicks always
  // open the same row instead of workflowsApi.create minting a fresh
  // duplicate one every time.
  const handleLoadTendrilWorkflow = useCallback(async () => {
    if (creatingTendril) return;
    setCreatingTendril(true);
    try {
      const workflowId = await tendril.console();
      router.push(`/workflows/${workflowId}`);
    } catch {
      setCreatingTendril(false);
    }
  }, [creatingTendril, router]);

  // Loads DEMO_WORKFLOW (lib/data.ts) into a brand-new workflow row every
  // click -- unlike handleLoadTendrilWorkflow's find-or-create console, a
  // demo is just a starting point the user immediately edits, so there's no
  // "the one shared demo" identity to preserve and a fresh copy each time is
  // correct. create() makes the empty row, then update() writes the full
  // node/edge graph in one shot (same two-call pattern the canvas editor's
  // own save path already uses).
  const handleLoadDemoWorkflow = useCallback(async () => {
    if (creatingDemo) return;
    setCreatingDemo(true);
    setPageError((prev) => (prev?.source === "demo" ? null : prev));
    let wf: Workflow | undefined;
    try {
      wf = await workflowsApi.create(DEMO_WORKFLOW.name);
      // UpdateWorkflow (backend/internal/api/handlers/workflows.go) overwrites
      // name unconditionally from the request body -- omitting it here would
      // blank out the name create() just set.
      await workflowsApi.update(wf.id, {
        name: DEMO_WORKFLOW.name,
        nodes: DEMO_WORKFLOW.nodes,
        edges: DEMO_WORKFLOW.edges,
      });
      router.push(`/workflows/${wf.id}`);
    } catch (e) {
      // If create() succeeded but update() failed, don't leave an empty
      // orphaned row behind in the user's workflow list -- best-effort
      // delete it before surfacing the error.
      if (wf) await workflowsApi.remove(wf.id).catch(() => {});
      setPageError({
        source: "demo",
        message:
          e instanceof Error ? e.message : "could not load demo workflow",
      });
      setCreatingDemo(false);
    }
  }, [creatingDemo, router]);

  // Deletion is permanent, so the row only calls this after its own in-menu
  // confirm step. The backend refuses (409) for workflows with Tendril lease
  // history; that message is shown rather than leaving the row silently intact.
  const handleDelete = useCallback(async (id: string) => {
    setPageError((prev) => (prev?.source === "delete" ? null : prev));
    try {
      await workflowsApi.remove(id);
      setWfList((prev) => prev.filter((w) => w.id !== id));
    } catch (e) {
      setPageError({
        source: "delete",
        message: e instanceof Error ? e.message : "could not delete workflow",
      });
    }
  }, []);

  return (
    <div
      style={{
        height: "100vh",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        background: "var(--bg)",
      }}
    >
      <Topbar />

      {/* Main */}
      <div style={{ flex: 1, overflow: "auto", background: "var(--bg)" }}>
        <div
          style={{
            maxWidth: 1280,
            margin: "0 auto",
            padding: "36px 24px 80px",
          }}
        >
          {/* Header */}
          <div
            style={{
              display: "flex",
              alignItems: "flex-end",
              justifyContent: "space-between",
              marginBottom: 28,
            }}
          >
            <div>
              <Tag>your workspace</Tag>
              <h1
                style={{
                  margin: "12px 0 4px",
                  fontSize: 36,
                  fontWeight: 500,
                  letterSpacing: "-0.025em",
                }}
              >
                Workflows
              </h1>
              <p style={{ margin: 0, color: "var(--fg-muted)", fontSize: 14 }}>
                Design, deploy, and monitor agent pipelines.
              </p>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              <button style={ghostBtn}>Import</button>
              <button
                onClick={handleLoadDemoWorkflow}
                disabled={creatingDemo}
                style={{
                  ...ghostBtn,
                  opacity: creatingDemo ? 0.6 : 1,
                  position: "relative",
                }}
                title="Two Gemini 2.5 Flash agents + an HTTP tool + a Telegram step (no-ops until you add your own bot token/chat ID) + up to 3 real CANIX402 x402 calls (Algorand mainnet) -- only 1 of those 3 is guaranteed, the other 2 fire only if the agent's LLM chooses to call them (and can fire more than once). $2.07 guaranteed floor, ~$5.09 typical, no fixed ceiling."
              >
                {creatingDemo ? "Loading…" : "Load demo workflow"}
                <span style={{ marginLeft: 6 }}>
                  <Pill tone="accent" mono>
                    $2.07+/run
                  </Pill>
                </span>
              </button>
              <button
                onClick={handleLoadTendrilWorkflow}
                disabled={creatingTendril}
                style={{
                  ...ghostBtn,
                  opacity: creatingTendril ? 0.6 : 1,
                  position: "relative",
                }}
                title="Rent a real Linux machine by the hour. SSH from the console. Official — built with Tendril."
              >
                {creatingTendril ? "Loading…" : "Load Tendril workflow"}
                <span
                  style={{
                    marginLeft: 6,
                    fontSize: 9,
                    fontFamily: "var(--font-mono)",
                    color: "#E879F9",
                    border: "1px solid #E879F9",
                    borderRadius: 999,
                    padding: "1px 5px",
                    textTransform: "uppercase",
                    letterSpacing: "0.04em",
                  }}
                >
                  Official
                </span>
              </button>
              <button
                onClick={handleNewWorkflow}
                disabled={creating}
                style={{ ...primaryBtn, opacity: creating ? 0.6 : 1 }}
              >
                {creating ? "Creating…" : "+ New workflow"}
              </button>
            </div>
          </div>

          {/* Credit balance — the one number that actually gates whether a run
              can happen here. Replaces the old KPI row, whose cards were all
              unwired placeholders. */}
          <Card
            style={{
              marginBottom: 24,
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 16,
            }}
          >
            <div>
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 7,
                  fontFamily: "var(--font-mono)",
                  fontSize: 10,
                  textTransform: "uppercase",
                  letterSpacing: "0.08em",
                  color: "var(--fg-dim)",
                }}
              >
                <IconWallet size={13} /> Credit balance
              </div>
              <div
                style={{
                  marginTop: 8,
                  fontSize: 28,
                  fontWeight: 500,
                  letterSpacing: "-0.02em",
                  fontFamily: "var(--font-mono)",
                  fontVariantNumeric: "tabular-nums",
                  color: "var(--fg)",
                }}
              >
                {balanceKnown ? `$${balanceUSD.toFixed(2)}` : "—"}
              </div>
              <div
                style={{ marginTop: 4, fontSize: 11, color: "var(--fg-muted)" }}
              >
                {balanceKnown
                  ? "Spent as your agents call paid tools and models."
                  : "Loading balance…"}
              </div>
            </div>
            <button onClick={() => router.push("/billing")} style={ghostBtn}>
              Add credits
            </button>
          </Card>

          {pageError && (
            <div
              style={{
                marginBottom: 16,
                padding: "10px 14px",
                borderRadius: "var(--r-2)",
                border: "1px solid var(--danger)",
                background: "var(--bg-elev-1)",
                color: "var(--danger)",
                fontSize: 12.5,
              }}
            >
              {pageError.message}
            </div>
          )}

          {/* Controls */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              marginBottom: 12,
            }}
          >
            <div style={{ position: "relative", flex: 1, maxWidth: 360 }}>
              <span
                style={{
                  position: "absolute",
                  left: 12,
                  top: 12,
                  color: "var(--fg-dim)",
                }}
              >
                <IconSearch size={12} />
              </span>
              <input
                style={{
                  height: 36,
                  paddingLeft: 32,
                  paddingRight: 12,
                  width: "100%",
                  background: "var(--bg-elev-1)",
                  border: "1px solid var(--border)",
                  borderRadius: "var(--r-2)",
                  color: "var(--fg)",
                  fontFamily: "var(--font-sans)",
                  fontSize: 13,
                  outline: "none",
                }}
                placeholder="Search workflows, tags…"
                value={q}
                onChange={(e) => setQ(e.target.value)}
              />
            </div>
            <div
              style={{
                display: "flex",
                gap: 2,
                background: "var(--bg-elev-1)",
                padding: 3,
                borderRadius: "var(--r-2)",
                border: "1px solid var(--border)",
              }}
            >
              {["all", "active", "paused", "draft"].map((s) => (
                <button
                  key={s}
                  onClick={() => setStatus(s)}
                  style={{
                    border: "none",
                    background:
                      status === s ? "var(--bg-elev-3)" : "transparent",
                    color: status === s ? "var(--fg)" : "var(--fg-muted)",
                    padding: "6px 12px",
                    fontSize: 12,
                    fontWeight: 500,
                    borderRadius: 5,
                    cursor: "pointer",
                    textTransform: "capitalize",
                    fontFamily: "var(--font-sans)",
                  }}
                >
                  {s}
                </button>
              ))}
            </div>
            <div style={{ flex: 1 }} />
            <div
              style={{
                display: "flex",
                gap: 2,
                background: "var(--bg-elev-1)",
                padding: 3,
                borderRadius: "var(--r-2)",
                border: "1px solid var(--border)",
              }}
            >
              <button
                onClick={() => setView("rows")}
                style={{
                  ...ghostBtnSm,
                  height: 26,
                  background:
                    view === "rows" ? "var(--bg-elev-3)" : "transparent",
                  border: "none",
                }}
              >
                ☰ Rows
              </button>
              <button
                onClick={() => setView("grid")}
                style={{
                  ...ghostBtnSm,
                  height: 26,
                  background:
                    view === "grid" ? "var(--bg-elev-3)" : "transparent",
                  border: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: 4,
                }}
              >
                <IconGrid size={11} /> Grid
              </button>
            </div>
          </div>

          {/* List */}
          {loading ? (
            <div
              style={{
                padding: 48,
                textAlign: "center",
                color: "var(--fg-dim)",
                fontFamily: "var(--font-mono)",
                fontSize: 12,
              }}
            >
              loading workflows…
            </div>
          ) : view === "rows" ? (
            <WorkflowRows
              items={filtered}
              onOpen={(id) => router.push(`/workflows/${id}`)}
              onDelete={handleDelete}
            />
          ) : (
            <WorkflowGrid
              items={filtered}
              onOpen={(id) => router.push(`/workflows/${id}`)}
            />
          )}

          {!loading && filtered.length === 0 && (
            <div
              style={{
                padding: 48,
                textAlign: "center",
                border: "1px dashed var(--border)",
                borderRadius: "var(--r-3)",
                color: "var(--fg-dim)",
                fontFamily: "var(--font-mono)",
                fontSize: 12,
              }}
            >
              {wfList.length === 0
                ? "no workflows yet, create one to get started"
                : "no workflows match"}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status?: string }) {
  const map: Record<
    string,
    { tone: "ok" | "warm" | "default"; label: string }
  > = {
    active: { tone: "ok", label: "Active" },
    paused: { tone: "warm", label: "Paused" },
    draft: { tone: "default", label: "Draft" },
  };
  const s = map[status ?? "draft"] ?? map.draft;
  return (
    <Pill tone={s.tone} dot mono>
      {s.label}
    </Pill>
  );
}

function WorkflowIcon({ name }: { name: string }) {
  const seed = name.charCodeAt(0) + name.length;
  const dotCount = 3 + (seed % 3);
  const dots = Array.from({ length: dotCount }, (_, i) => {
    const a = (i / dotCount) * Math.PI * 2 + seed * 0.3;
    return { x: 14 + Math.cos(a) * 8, y: 14 + Math.sin(a) * 8 };
  });
  return (
    <div
      style={{
        width: 36,
        height: 36,
        borderRadius: 8,
        background: "var(--bg-elev-3)",
        border: "1px solid var(--border-strong)",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        flexShrink: 0,
      }}
    >
      <svg width="28" height="28" viewBox="0 0 28 28">
        {dots.map((d, i) =>
          dots.map((d2, j) =>
            j > i ? (
              <line
                key={`${i}_${j}`}
                x1={d.x}
                y1={d.y}
                x2={d2.x}
                y2={d2.y}
                stroke="var(--fg-dim)"
                strokeWidth="0.5"
              />
            ) : null,
          ),
        )}
        {dots.map((d, i) => (
          <circle
            key={i}
            cx={d.x}
            cy={d.y}
            r="2"
            fill={i === 0 ? "var(--accent)" : "var(--fg-muted)"}
          />
        ))}
      </svg>
    </div>
  );
}

// RowMenu is the ⋯ menu on a workflow row. Delete is permanent, so it asks for
// a second click ("Delete permanently?") in place rather than firing on the
// first — and rather than a browser confirm() dialog, which the rest of the app
// doesn't use.
function RowMenu({ onDelete }: { onDelete: () => void }) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  // The rows list scrolls horizontally (overflow-x: auto), which clips absolutely
  // positioned children in both axes — so the menu is position:fixed, anchored to
  // the button's viewport rect, and closes on scroll/resize rather than drifting.
  const [anchor, setAnchor] = useState<{ top: number; right: number } | null>(
    null,
  );
  const ref = useRef<HTMLDivElement>(null);
  const btnRef = useRef<HTMLButtonElement>(null);

  const close = useCallback(() => {
    setOpen(false);
    setConfirming(false);
  }, []);

  // Close on any click outside, so an open menu can't be left hanging over a
  // row the user has moved on from.
  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
    };
  }, [open, close]);

  return (
    <div
      ref={ref}
      // The row itself navigates on click; nothing inside this menu should.
      onClick={(e) => e.stopPropagation()}
    >
      <button
        ref={btnRef}
        aria-label="Workflow actions"
        aria-haspopup="menu"
        aria-expanded={open}
        style={{
          ...ghostBtnSm,
          width: 28,
          padding: 0,
          justifyContent: "center",
          background: open ? "var(--bg-elev-3)" : undefined,
        }}
        onClick={() => {
          if (open) {
            close();
            return;
          }
          const rect = btnRef.current?.getBoundingClientRect();
          if (rect) {
            setAnchor({
              top: rect.bottom + 6,
              right: window.innerWidth - rect.right,
            });
          }
          setConfirming(false);
          setOpen(true);
        }}
      >
        ⋯
      </button>
      {open && anchor && (
        <div
          role="menu"
          style={{
            position: "fixed",
            top: anchor.top,
            right: anchor.right,
            zIndex: 40,
            minWidth: 168,
            padding: 4,
            background: "var(--bg-elev-2)",
            border: "1px solid var(--border-strong)",
            borderRadius: "var(--r-2)",
            boxShadow: "0 12px 32px rgba(0,0,0,0.45)",
          }}
        >
          <button
            role="menuitem"
            onClick={() => {
              if (!confirming) {
                setConfirming(true);
                return;
              }
              close();
              onDelete();
            }}
            style={{
              display: "block",
              width: "100%",
              textAlign: "left",
              padding: "8px 10px",
              border: "none",
              borderRadius: 5,
              background: confirming
                ? "var(--danger-soft, transparent)"
                : "transparent",
              color: "var(--danger)",
              fontSize: 12.5,
              fontWeight: confirming ? 600 : 500,
              fontFamily: "var(--font-sans)",
              cursor: "pointer",
            }}
            onMouseEnter={(e) =>
              (e.currentTarget.style.background = "var(--bg-elev-3)")
            }
            onMouseLeave={(e) =>
              (e.currentTarget.style.background = confirming
                ? "var(--danger-soft, transparent)"
                : "transparent")
            }
          >
            {confirming ? "Delete permanently?" : "Delete workflow"}
          </button>
          {confirming && (
            <button
              role="menuitem"
              onClick={() => setConfirming(false)}
              style={{
                display: "block",
                width: "100%",
                textAlign: "left",
                padding: "8px 10px",
                border: "none",
                borderRadius: 5,
                background: "transparent",
                color: "var(--fg-muted)",
                fontSize: 12.5,
                fontFamily: "var(--font-sans)",
                cursor: "pointer",
              }}
            >
              Cancel
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function WorkflowRows({
  items,
  onOpen,
  onDelete,
}: {
  items: Workflow[];
  onOpen: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  return (
    <Card style={{ padding: 0, overflowX: "auto" }}>
      <div
        style={{
          display: "grid",
          gridTemplateColumns:
            "minmax(180px, 240px) minmax(96px, 1fr) minmax(80px, 1fr) minmax(96px, 1fr) minmax(110px, 1fr) minmax(120px, 1fr) 80px",
          gap: 12,
          padding: "10px 16px",
          background: "var(--bg-elev-2)",
          borderBottom: "1px solid var(--border)",
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          textTransform: "uppercase",
          letterSpacing: "0.08em",
          color: "var(--fg-dim)",
        }}
      >
        <span>Name</span>
        <span>Status</span>
        <span>Agents</span>
        <span>Runs · 30d</span>
        <span>Spend · 30d</span>
        <span>Updated</span>
        <span></span>
      </div>
      {items.map((wf, i) => (
        <div
          key={wf.id}
          onClick={() => onOpen(wf.id)}
          style={{
            display: "grid",
            gridTemplateColumns:
              "minmax(180px, 240px) minmax(96px, 1fr) minmax(80px, 1fr) minmax(96px, 1fr) minmax(110px, 1fr) minmax(120px, 1fr) 80px",
            gap: 12,
            padding: "14px 16px",
            alignItems: "center",
            borderBottom:
              i < items.length - 1 ? "1px solid var(--border-soft)" : "none",
            cursor: "pointer",
            transition: "background .12s",
          }}
          onMouseEnter={(e) =>
            (e.currentTarget.style.background = "var(--bg-elev-2)")
          }
          onMouseLeave={(e) =>
            (e.currentTarget.style.background = "transparent")
          }
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 12,
              minWidth: 0,
            }}
          >
            <WorkflowIcon name={wf.name ?? ""} />
            <div style={{ minWidth: 0 }}>
              <div
                style={{
                  fontSize: 14,
                  fontWeight: 500,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {wf.name}
              </div>
              <div style={{ display: "flex", gap: 5, marginTop: 4 }}>
                {wf.tags?.map((t) => (
                  <span
                    key={t}
                    style={{
                      fontFamily: "var(--font-mono)",
                      fontSize: 9,
                      color: "var(--fg-dim)",
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                    }}
                  >
                    #{t}
                  </span>
                ))}
              </div>
            </div>
          </div>
          <StatusBadge status={wf.status} />
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
            {wf.agents ??
              wf.nodes?.filter((n) => n.type === "agent").length ??
              0}
          </span>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              color: "var(--fg-muted)",
            }}
          >
            {wf.runs?.toLocaleString() ?? "-"}
          </span>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              color: "var(--accent)",
            }}
          >
            {wf.spend ?? "-"}
            {wf.spend && <span style={{ color: "var(--fg-dim)" }}> ALGO</span>}
          </span>
          <span
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-muted)",
            }}
          >
            {fmtDate(wf.updatedAt ?? wf.updated)}
          </span>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 4 }}>
            <button
              style={ghostBtnSm}
              onClick={(e) => {
                e.stopPropagation();
                onOpen(wf.id);
              }}
            >
              Open
            </button>
            <RowMenu onDelete={() => onDelete(wf.id)} />
          </div>
        </div>
      ))}
    </Card>
  );
}

function WorkflowGrid({
  items,
  onOpen,
}: {
  items: Workflow[];
  onOpen: (id: string) => void;
}) {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(3, 1fr)",
        gap: 16,
      }}
    >
      {items.map((wf) => (
        <Card
          key={wf.id}
          onClick={() => onOpen(wf.id)}
          style={{
            cursor: "pointer",
            transition: "border-color .15s, transform .15s",
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLElement).style.borderColor =
              "var(--border-strong)";
            (e.currentTarget as HTMLElement).style.transform =
              "translateY(-2px)";
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLElement).style.borderColor =
              "var(--border)";
            (e.currentTarget as HTMLElement).style.transform = "translateY(0)";
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <WorkflowIcon name={wf.name ?? ""} />
            <StatusBadge status={wf.status} />
          </div>
          <div
            style={{
              marginTop: 16,
              fontSize: 16,
              fontWeight: 500,
              letterSpacing: "-0.015em",
            }}
          >
            {wf.name}
          </div>
          <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
            {wf.tags?.map((t) => (
              <span
                key={t}
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 9,
                  color: "var(--fg-dim)",
                  textTransform: "uppercase",
                  letterSpacing: "0.06em",
                }}
              >
                #{t}
              </span>
            ))}
          </div>
          <div
            style={{
              marginTop: 16,
              paddingTop: 12,
              borderTop: "1px solid var(--border-soft)",
              display: "grid",
              gridTemplateColumns: "repeat(3, 1fr)",
              gap: 8,
              fontFamily: "var(--font-mono)",
              fontSize: 11,
            }}
          >
            {[
              {
                label: "Agents",
                val: String(
                  wf.agents ??
                    wf.nodes?.filter((n) => n.type === "agent").length ??
                    0,
                ),
              },
              { label: "Runs", val: wf.runs?.toLocaleString() ?? "-" },
              { label: "Spend", val: wf.spend ?? "-", accent: true },
            ].map((s) => (
              <div key={s.label}>
                <div
                  style={{
                    color: "var(--fg-dim)",
                    fontSize: 9,
                    textTransform: "uppercase",
                    letterSpacing: "0.06em",
                  }}
                >
                  {s.label}
                </div>
                <div
                  style={{
                    color: s.accent ? "var(--accent)" : "var(--fg)",
                    marginTop: 2,
                  }}
                >
                  {s.val}
                </div>
              </div>
            ))}
          </div>
        </Card>
      ))}
    </div>
  );
}

function fmtDate(iso?: string): string {
  if (!iso) return "-";
  try {
    return new Intl.DateTimeFormat("en", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }).format(new Date(iso));
  } catch {
    return iso;
  }
}

// Shared styles
const ghostBtn: React.CSSProperties = {
  height: 36,
  padding: "0 14px",
  fontSize: 13,
  fontWeight: 500,
  background: "var(--bg-elev-2)",
  border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)",
  color: "var(--fg)",
  cursor: "pointer",
  fontFamily: "var(--font-sans)",
  display: "inline-flex",
  alignItems: "center",
};
const primaryBtn: React.CSSProperties = {
  height: 36,
  padding: "0 14px",
  fontSize: 13,
  fontWeight: 600,
  background: "var(--accent)",
  border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)",
  color: "var(--accent-fg)",
  cursor: "pointer",
  fontFamily: "var(--font-sans)",
  display: "inline-flex",
  alignItems: "center",
};
