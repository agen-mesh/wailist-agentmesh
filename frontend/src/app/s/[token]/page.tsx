import type { Metadata } from "next";
import { SharePreview } from "@/components/share/SharePreview";
import { MOBILE_SHELL_ID } from "@/lib/routes";
import type { ShareImportRequirements, WorkflowShare } from "@/lib/types";

// The public end of a share link.
//
// /s sits OUTSIDE the PROTECTED prefixes in middleware.ts, deliberately and
// not by accident: every other workflow route bounces a signed-out visitor to
// /signin, which would make a link posted anywhere public show a stranger
// nothing but a login wall. Adding /s to that list would silently break the
// feature, so if you are here to do that, read SharePreview first.
//
// The native shell ships a static export, which cannot prerender a page per
// token -- the tokens belong to links that do not exist at build time. Unlike
// /workflows/[id], though, this shell page is not a working screen: nothing in
// the app opens a share link inside the WebView, and lib/readonly.ts blocks
// importing on native regardless. It exists only because `output: export`
// refuses to build a dynamic route whose generateStaticParams returns nothing
// ("at least one route must be generated"), which is a build failure the
// android-pr workflow would have caught after this had already merged.
//
// The web build returns no params and keeps rendering every token on demand,
// exactly as /workflows/[id] does.
export function generateStaticParams() {
  return process.env.MOBILE_BUILD === "1" ? [{ token: MOBILE_SHELL_ID }] : [];
}

type ShareResponse = {
  share: WorkflowShare;
  requirements: ShareImportRequirements;
};

// Read the share on the SERVER, so the page has content before any JavaScript
// runs.
//
// This is what makes a link worth pasting anywhere. Slack, WhatsApp, X,
// Discord and iMessage all fetch the URL and read the HTML; none of them run
// React. While this page fetched on mount, every one of them saw an empty
// shell, so a shared workflow unfurled as a bare URL -- and every crawler saw
// the same nothing.
//
// BACKEND_URL is the server-only variable next.config.ts already proxies
// /api/* to. It is absent in mock mode and during the static mobile export,
// and both simply fall through to the client fetch that was always here.
async function loadShare(token: string): Promise<ShareResponse | null> {
  const base = process.env.BACKEND_URL;
  if (!base || process.env.MOBILE_BUILD === "1") return null;
  try {
    const res = await fetch(
      `${base.replace(/\/$/, "")}/shares/${encodeURIComponent(token)}`,
      // Never cached: a link can be revoked at any moment, and a preview
      // served from cache would keep advertising a workflow whose owner has
      // already pulled it back.
      { cache: "no-store" },
    );
    if (!res.ok) return null;
    return (await res.json()) as ShareResponse;
  } catch {
    // A dead backend must not take the page down with it -- the client fetch
    // still runs and reports the failure in the page's own words.
    return null;
  }
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ token: string }>;
}): Promise<Metadata> {
  const { token } = await params;
  const data = await loadShare(token);

  // Missing, revoked and expired are one case here, exactly as they are in
  // the API: an unfurl must not become the oracle that tells somebody whether
  // a token ever existed. The generic card says nothing either way.
  if (!data) {
    return {
      title: "Shared workflow",
      description: "Open this link to see the workflow and import a copy.",
    };
  }

  const { share } = data;
  const size = `${share.nodeCount} nodes · ${share.edgeCount} connections`;
  const description = share.description?.trim()
    ? share.description.trim()
    : `An AgentMesh workflow — ${size}. Import your own copy.`;

  return {
    title: `${share.name} — shared on AgentMesh`,
    description,
    openGraph: {
      title: share.name,
      description,
      type: "website",
      siteName: "AgentMesh",
    },
    twitter: {
      card: "summary_large_image",
      title: share.name,
      description,
    },
  };
}

export default async function SharePageRoute({
  params,
}: {
  params: Promise<{ token: string }>;
}) {
  const { token } = await params;
  const initial = await loadShare(token);
  // No Suspense boundary here, unlike /workflows/[id]: that one wraps a tree
  // that calls useSearchParams(), which Next 16 refuses to build without one.
  // SharePreview reads nothing from the query string, so a boundary would be
  // ceremony. Add one the moment it does.
  return <SharePreview token={token} initial={initial ?? undefined} />;
}
