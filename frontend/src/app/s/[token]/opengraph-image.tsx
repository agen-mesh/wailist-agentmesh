import { ImageResponse } from "next/og";

// The picture Slack, WhatsApp, X, Discord and iMessage show when somebody
// pastes a share link.
//
// A per-share image rather than the site-wide banner, because the whole point
// of the link is the particular workflow on the other end of it. Seeing its
// name and size in the unfurl is the difference between "someone sent a link"
// and "someone sent me a resume screener with 6 nodes".
//
// This file overrides the root opengraph-image.png for /s/* only. The file
// convention wins over anything `openGraph.images` sets in metadata, which is
// why generateMetadata in page.tsx sets no images of its own.

export const alt = "A workflow shared on AgentMesh";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

// The static mobile export cannot run an image route per token, and nothing
// in the WebView opens a share link anyway -- one shell keeps `output:
// export` satisfied, exactly as page.tsx does.
export function generateStaticParams() {
  return process.env.MOBILE_BUILD === "1" ? [{ token: "app" }] : [];
}

// Tokens copied, not imported: this renders in Satori, which never loads the
// stylesheet. Kept in step with globals.css by hand -- there are five.
const BG = "#08070c";
const BORDER = "#26243a";
const FG = "#f2f0f7";
const MUTED = "#8e8aa3";
const ACCENT = "#a78bfa";

type Share = {
  name: string;
  nodeCount: number;
  edgeCount: number;
};

async function loadShare(token: string): Promise<Share | null> {
  const base = process.env.BACKEND_URL;
  if (!base || process.env.MOBILE_BUILD === "1") return null;
  try {
    const res = await fetch(
      `${base.replace(/\/$/, "")}/shares/${encodeURIComponent(token)}`,
      { cache: "no-store" },
    );
    if (!res.ok) return null;
    const data = (await res.json()) as { share: Share };
    return data.share;
  } catch {
    return null;
  }
}

export default async function Image({
  params,
}: {
  params: Promise<{ token: string }>;
}) {
  const { token } = await params;
  const share = await loadShare(token);

  // A dead or missing link gets the neutral card. Same rule as the 404 and
  // the metadata: an unfurl must not reveal whether a token ever existed.
  const title = share?.name ?? "A workflow on AgentMesh";
  const sub = share
    ? `${share.nodeCount} nodes · ${share.edgeCount} connections`
    : "Design, deploy, and monitor AI agent workflows.";

  return new ImageResponse(
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        justifyContent: "space-between",
        background: BG,
        padding: 72,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
        <div
          style={{
            width: 44,
            height: 44,
            borderRadius: 10,
            background: ACCENT,
            display: "flex",
          }}
        />
        <div style={{ fontSize: 30, fontWeight: 600, color: FG }}>
          AgentMesh
        </div>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
        <div style={{ fontSize: 26, color: ACCENT, letterSpacing: 2 }}>
          SHARED WORKFLOW
        </div>
        <div
          style={{
            display: "flex",
            fontSize: 68,
            fontWeight: 700,
            color: FG,
            lineHeight: 1.1,
            // Satori has no text-overflow, so a very long name is clipped
            // by the box rather than pushed off the canvas.
            maxHeight: 230,
            overflow: "hidden",
          }}
        >
          {title}
        </div>
        <div style={{ fontSize: 30, color: MUTED }}>{sub}</div>
      </div>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          borderTop: `1px solid ${BORDER}`,
          paddingTop: 28,
          fontSize: 26,
          color: MUTED,
        }}
      >
        <div style={{ display: "flex" }}>Import your own copy</div>
        <div style={{ display: "flex" }}>agent-mesh.app</div>
      </div>
    </div>,
    size,
  );
}
