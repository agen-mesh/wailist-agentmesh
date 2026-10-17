import type { Metadata } from "next";
import { SharePreview } from "@/components/share/SharePreview";
import { MOBILE_SHELL_ID } from "@/lib/routes";
import { loadServerShare } from "@/lib/serverShare";

// The native export needs one shell. An empty web list would select runtime
// static generation, which rejects the uncached share lookup for new tokens.
export const generateStaticParams =
  process.env.MOBILE_BUILD === "1"
    ? () => [{ token: MOBILE_SHELL_ID }]
    : undefined;

export async function generateMetadata({
  params,
}: {
  params: Promise<{ token: string }>;
}): Promise<Metadata> {
  const { token } = await params;
  const data = await loadServerShare(token);

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
    : `An AgentMesh workflow, ${size}. Import your own copy.`;

  return {
    title: `${share.name}, shared on AgentMesh`,
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
  const initial = await loadServerShare(token);
  // No Suspense boundary here, unlike /workflows/[id]: that one wraps a tree
  // that calls useSearchParams(), which Next 16 refuses to build without one.
  // SharePreview reads nothing from the query string, so a boundary would be
  // ceremony. Add one the moment it does.
  return <SharePreview token={token} initial={initial ?? undefined} />;
}
