import { Tag } from "@/components/ui";
import { requirementLines } from "@/lib/shareRequirements";
import type {
  ShareImportRequirements,
  WorkflowEdge,
  WorkflowNode,
} from "@/lib/types";
import { ShareGraphSketch } from "./ShareGraphSketch";

// One workflow, described once.
//
// Two surfaces ask the same question -- the public page behind a link, and the
// Import dialog, which shows what a pasted link or code holds before it
// creates anything. They used to answer it in two different visual languages,
// the dialog's a flatter copy of the page's, which is the situation that put
// requirementLines in lib/shareRequirements: two copies drift, and the drift
// is invisible because both go on rendering something plausible.
//
// The variants differ only where nesting forces it. In the dialog the card is
// already inside a titled panel, so it drops the eyebrow and the node-name
// tags rather than stacking a third label under a heading and a subtitle.

// How a node type reads to somebody who has never used the app. The catalogue
// in lib/data.ts is keyed for the canvas palette and carries geometry we have
// no use for here, so this is a small separate mapping rather than a reach
// into that.
const NODE_LABELS: Record<string, string> = {
  trigger: "Trigger",
  agent: "Agent",
  provider: "Model",
  tool: "Tool",
  tool402: "Paid tool",
  action: "Action",
  state: "State",
  end: "End",
  tendril: "Compute",
  google: "Google",
};

function expiryNote(expiresAt?: string): string | null {
  if (!expiresAt) return null;
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms)) return null;
  const days = Math.ceil(ms / 86_400_000);
  if (days <= 0) return "Expires today";
  return days === 1 ? "Expires tomorrow" : `Expires in ${days} days`;
}

export function SharedWorkflowCard({
  name,
  description,
  nodeCount,
  edgeCount,
  graph,
  requirements,
  expiresAt,
  variant,
  className,
}: {
  name: string;
  description?: string;
  nodeCount: number;
  edgeCount: number;
  graph?: { nodes: WorkflowNode[]; edges: WorkflowEdge[] };
  /** Absent means nobody has worked them out -- which is the case for a
   *  pasted code. They are derived server-side from a stored snapshot, and a
   *  code has no row anywhere until it is imported. The three headings below
   *  turn on exactly that distinction, so it must not be flattened to an
   *  empty object. */
  requirements?: ShareImportRequirements;
  expiresAt?: string;
  variant: "page" | "dialog";
  className?: string;
}) {
  const page = variant === "page";
  const lines = requirements ? requirementLines(requirements) : [];
  const expires = expiryNote(expiresAt);
  const nodes = graph?.nodes ?? [];

  return (
    <div className={className} style={{ marginBottom: page ? 20 : 14 }}>
      {page && <p style={eyebrow}>Shared workflow</p>}
      {/* The workflow name is what the public page is ABOUT, so there it is
          the h1. In the dialog it names a section inside a panel that already
          has its own heading, so it is a paragraph rather than a second one
          competing with it. */}
      {page ? (
        <h1 style={pageName}>{name}</h1>
      ) : (
        <p style={dialogName}>{name}</p>
      )}
      {description && (
        <p style={page ? pageDescription : dialogDescription}>{description}</p>
      )}

      <div style={page ? pageMeta : dialogMeta}>
        <span>{nodeCount} nodes</span>
        <span style={{ color: "var(--fg-dim)" }}>·</span>
        <span>{edgeCount} connections</span>
        {expires && (
          <>
            <span style={{ color: "var(--fg-dim)" }}>·</span>
            <span style={{ color: "var(--warm)" }}>{expires}</span>
          </>
        )}
      </div>

      {graph && <ShareGraphSketch nodes={graph.nodes} edges={graph.edges} />}

      {page && nodes.length > 0 && (
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: "var(--s-2)",
            marginBottom: 20,
          }}
        >
          {nodes.slice(0, 12).map((n) => (
            <Tag key={n.id}>
              {n.label || n.name || NODE_LABELS[n.type] || n.type}
            </Tag>
          ))}
          {nodes.length > 12 && <Tag>+{nodes.length - 12} more</Tag>}
        </div>
      )}

      {/* A hairline rather than a bordered, filled box. What this says matters
          and it stays; a fourth border inside a panel that already has three
          was carrying none of that weight. */}
      <div style={rule} />

      {/* "Ready to run as-is" is a claim, and for a code nothing has checked
          it -- so a code gets neither of the other two headings. Saying it
          anyway would be the false promise this feature already had to remove
          from the link preview, reintroduced one branch over. */}
      <p style={reqHeading}>
        {!requirements
          ? "What it may still need"
          : lines.length > 0
            ? "You'll need to add"
            : "Ready to run as-is"}
      </p>
      {lines.length > 0 ? (
        <ul style={reqList}>
          {lines.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      ) : (
        <p style={reqBody}>
          {requirements
            ? "Nothing to configure — this one runs on AgentMesh's own model keys."
            : "The sender's keys, secrets and uploaded files were never part of this code, so any model keys or connected accounts it uses will be yours to add. Open it after importing to see which."}
        </p>
      )}
      {requirements && (
        <p style={reqFootnote}>
          The sender&apos;s API keys, secrets and uploaded files were never part
          of this link.
        </p>
      )}
    </div>
  );
}

const eyebrow: React.CSSProperties = {
  margin: "0 0 6px",
  fontSize: "var(--t-1)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
};

const pageName: React.CSSProperties = {
  fontSize: "var(--t-6)",
  fontWeight: 700,
  margin: "0 0 10px",
  letterSpacing: "-0.02em",
};

const dialogName: React.CSSProperties = {
  fontSize: "var(--t-4)",
  fontWeight: 700,
  margin: "0 0 4px",
  letterSpacing: "-0.01em",
};

const pageDescription: React.CSSProperties = {
  margin: "0 0 14px",
  fontSize: "var(--t-3)",
  lineHeight: 1.6,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const dialogDescription: React.CSSProperties = {
  margin: "0 0 8px",
  fontSize: "var(--t-2)",
  lineHeight: 1.6,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const metaBase: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "var(--s-3)",
  flexWrap: "wrap",
  fontFamily: "var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  color: "var(--fg-muted)",
};

const pageMeta: React.CSSProperties = {
  ...metaBase,
  marginBottom: 20,
  fontSize: "var(--t-2)",
};

const dialogMeta: React.CSSProperties = {
  ...metaBase,
  marginBottom: 12,
  fontSize: "var(--t-1)",
};

const rule: React.CSSProperties = {
  height: 1,
  background: "var(--border)",
  margin: "0 0 12px",
};

const reqHeading: React.CSSProperties = {
  margin: "0 0 5px",
  fontSize: "var(--t-2)",
  fontWeight: 600,
};

const reqList: React.CSSProperties = {
  margin: 0,
  paddingLeft: 17,
  fontSize: "var(--t-2)",
  lineHeight: 1.75,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const reqBody: React.CSSProperties = {
  margin: 0,
  fontSize: "var(--t-2)",
  lineHeight: 1.6,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
};

const reqFootnote: React.CSSProperties = {
  margin: "9px 0 0",
  fontSize: "var(--t-1)",
  lineHeight: 1.6,
  color: "var(--fg-dim)",
  maxWidth: "60ch",
};
