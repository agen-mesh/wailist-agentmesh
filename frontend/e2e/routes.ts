// Every page route under src/app, as a path a browser can open.
//
// /auth/callback is left out: it only acts on the parameters an OAuth provider
// sends back, and without them it redirects straight away.
//
// Dynamic segments use a fixed id. In mock mode lib/api.ts answers
// workflows.get() with the sample workflow whatever the id is.
export const WORKFLOW_ID = "smoke";

export const PUBLIC_ROUTES = [
  "/",
  "/signin",
  "/signup",
  "/privacy",
  "/terms",
  "/refund-policy",
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
] as const;

export const ALL_ROUTES: readonly string[] = [
  ...PUBLIC_ROUTES,
  ...PROTECTED_ROUTES,
];
