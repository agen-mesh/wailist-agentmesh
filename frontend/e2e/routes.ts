// Every page route under src/app, as a path a browser can open.
//
// /auth/callback is left out: it only acts on the parameters an OAuth provider
// sends back, and without them it redirects straight away.
//
// Dynamic segments use a fixed id. In mock mode lib/api.ts answers
// workflows.get() with the sample workflow whatever the id is.
export const WORKFLOW_ID = "smoke";
export const SHARE_TOKEN = "smoke-sharing-token";

export function pageRoute(segments: readonly string[]): string {
  return "/" + segments
    .filter((segment) => !segment.startsWith("("))
    .map((segment) => {
      if (segment === "[id]") return WORKFLOW_ID;
      if (segment === "[token]") return SHARE_TOKEN;
      if (segment.includes("[")) {
        throw new Error(`No smoke sample for dynamic segment ${segment}`);
      }
      return segment;
    })
    .join("/");
}

export const PUBLIC_ROUTES = [
  "/",
  "/signin",
  "/signup",
  "/privacy",
  "/terms",
  "/refund-policy",
  "/delete-account",
  `/s/${SHARE_TOKEN}`,
] as const;

// Behind src/middleware.ts, which redirects to /signin unless the agentmesh_ui
// cookie is present. The spec sets that cookie before every test.
export const PROTECTED_ROUTES = [
  "/workflows",
  `/workflows/${WORKFLOW_ID}`,
  `/workflows/${WORKFLOW_ID}/geofence`,
  "/bazaar",
  "/bazaar/tendril",
  "/bazaar/prism",
  "/bazaar/helixbox",
  "/usage",
  "/billing",
  "/activity",
  "/account",
  "/account/links",
  "/settings",
] as const;

export const ALL_ROUTES: readonly string[] = [
  ...PUBLIC_ROUTES,
  ...PROTECTED_ROUTES,
];
