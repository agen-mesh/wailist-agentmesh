import { NextRequest, NextResponse } from "next/server";

const PROTECTED = [
  "/workflows",
  "/billing",
  "/usage",
  "/bazaar",
  "/activity",
  "/account",
  "/settings",
];
// agentmesh_ui is a non-sensitive first-party cookie set by useAuth on the
// frontend domain. The real auth is the HttpOnly agentmesh_token cookie sent
// directly to the backend -- that cookie lives on the API domain and is never
// visible here. agentmesh_ui is just the signal for this middleware check.
const AUTH_COOKIE = "agentmesh_ui";

// ── Maintenance mode ────────────────────────────────────────────────────────
//
// MAINTENANCE_MODE=on takes the public website down: every page is rewritten
// to /maintenance, so nobody outside the team sees the landing page, Sign In,
// Sign Up or the studio. Off unless explicitly switched on, so a deployment
// keeps behaving exactly as it does today until someone opts in.
//
// The team's way through is a shared secret, MAINTENANCE_BYPASS_TOKEN. Opening
// any page with ?bypass=<token> sets an HttpOnly cookie holding it, after which
// the site behaves normally for that browser. ?bypass=off clears it, to see
// what the public sees. Rotating the token locks every existing bypass out.
//
// Web only, by construction: the native app ships as a static export, and
// middleware never runs there. A frontend gate, not a backend one -- the API
// is not behind this, and the /api proxy is excluded from the matcher below so
// a bypassed browser can still reach it.
const MAINTENANCE_PAGE = "/maintenance";
const BYPASS_COOKIE = "agentmesh_maintenance_bypass";
const BYPASS_PARAM = "bypass";
const BYPASS_MAX_AGE = 60 * 60 * 24 * 7;
// Pages that stay public during maintenance. Google's OAuth verification needs
// /privacy reachable at all times, and the maintenance page links all three.
const MAINTENANCE_PUBLIC = ["/terms", "/privacy", "/refund-policy"];

// Read per request rather than at module load, so the switch is whatever the
// deployment's environment says now.
function maintenanceOn(): boolean {
  return process.env.MAINTENANCE_MODE?.trim().toLowerCase() === "on";
}

function bypassToken(): string {
  return process.env.MAINTENANCE_BYPASS_TOKEN?.trim() ?? "";
}

// Constant-time so a guess cannot be refined one character at a time from
// response timing. Length still leaks, which a long random token makes moot.
function tokenMatches(candidate: string | undefined, token: string): boolean {
  if (!token || !candidate || candidate.length !== token.length) return false;
  let diff = 0;
  for (let i = 0; i < token.length; i++) {
    diff |= candidate.charCodeAt(i) ^ token.charCodeAt(i);
  }
  return diff === 0;
}

function maintenance(req: NextRequest): NextResponse | null {
  const { pathname, searchParams } = req.nextUrl;

  if (!maintenanceOn()) {
    // The page only means something while the site is down. Anyone landing on
    // it afterwards -- a bookmark, a stale tab -- belongs on the real site.
    if (pathname === MAINTENANCE_PAGE) {
      return NextResponse.redirect(new URL("/", req.url));
    }
    return null;
  }

  const token = bypassToken();
  const attempt = searchParams.get(BYPASS_PARAM);
  if (attempt !== null) {
    // Strip the parameter either way, so the secret does not sit in the
    // address bar, the history, or a link someone copies from it.
    const clean = req.nextUrl.clone();
    clean.searchParams.delete(BYPASS_PARAM);
    if (attempt === "off") {
      const res = NextResponse.redirect(clean);
      res.cookies.delete(BYPASS_COOKIE);
      return res;
    }
    if (tokenMatches(attempt, token)) {
      const res = NextResponse.redirect(clean);
      res.cookies.set(BYPASS_COOKIE, token, {
        httpOnly: true,
        secure: req.nextUrl.protocol === "https:",
        sameSite: "lax",
        path: "/",
        maxAge: BYPASS_MAX_AGE,
      });
      return res;
    }
    // A wrong token is treated as no token: the visitor gets the maintenance
    // page like anyone else, with nothing to say a guess was close.
  }

  if (tokenMatches(req.cookies.get(BYPASS_COOKIE)?.value, token)) return null;

  const isPublic =
    pathname === MAINTENANCE_PAGE ||
    MAINTENANCE_PUBLIC.some(
      (p) => pathname === p || pathname.startsWith(p + "/"),
    );
  if (isPublic) return NextResponse.next();

  // A rewrite, not a redirect: the visitor keeps the URL they asked for, and
  // the same URL serves the real page again the moment maintenance ends.
  const url = req.nextUrl.clone();
  url.pathname = MAINTENANCE_PAGE;
  url.search = "";
  return NextResponse.rewrite(url);
}

export function middleware(req: NextRequest) {
  const gated = maintenance(req);
  if (gated) return gated;

  const { pathname } = req.nextUrl;

  const isProtected = PROTECTED.some(
    (p) => pathname === p || pathname.startsWith(p + "/"),
  );

  if (isProtected && !req.cookies.get(AUTH_COOKIE)?.value) {
    const url = req.nextUrl.clone();
    url.pathname = "/signin";
    // Keep the query string: deep links like /usage?workflow=<id> must survive
    // the auth round-trip or the page loses its filter after sign-in.
    url.search = "";
    url.searchParams.set("next", pathname + req.nextUrl.search);
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    "/workflows",
    "/workflows/:path*",
    "/billing",
    "/billing/:path*",
    "/usage",
    "/usage/:path*",
    "/bazaar",
    "/bazaar/:path*",
    "/activity",
    "/activity/:path*",
    "/account",
    "/account/:path*",
    "/settings",
    "/settings/:path*",
    // Every other page, for maintenance mode -- the entries above are what
    // the sign-in check needs, this is what taking the whole site down needs.
    // Excludes the /api proxy, Next's own assets and any file with an
    // extension (logo, favicon, og:image, .well-known/*.json, llms.txt), so
    // the maintenance page can still load its logo and fonts.
    "/((?!api/|_next/|.*\\.[a-zA-Z0-9]+$).*)",
  ],
};
