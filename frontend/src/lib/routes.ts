import { IS_NATIVE } from "./nativeAuth";

// Where a workflow lives, as a URL the running build can open.
//
// The web app serves every workflow at /workflows/<id>. The native shell is a
// static export: it cannot prerender a page per workflow, so it ships one shell
// page per route, /workflows/app, and reads the real id from ?id=
// (WorkflowRouteFromUrl, GeofenceRouteFromUrl). A native link to
// /workflows/<id> asks the WebView for a file that does not exist, and the app
// falls back to reloading the page it was on. Every in-app link to a workflow
// goes through here so the two forms cannot drift apart.
export const MOBILE_SHELL_ID = "app";

export function workflowHref(
  id: string,
  options: { geofence?: boolean; query?: Record<string, string> } = {},
): string {
  const params = new URLSearchParams();
  let path: string;
  if (IS_NATIVE) {
    path = `/workflows/${MOBILE_SHELL_ID}`;
    params.set("id", id);
  } else {
    path = `/workflows/${encodeURIComponent(id)}`;
  }
  if (options.geofence) path += "/geofence";
  for (const [key, value] of Object.entries(options.query ?? {})) {
    // The id is the one parameter a caller must not be able to replace.
    if (key !== "id") params.set(key, value);
  }
  const search = params.toString();
  return search ? `${path}?${search}` : path;
}

// Where a share link lives. Short on purpose -- this is the one URL in the app
// meant to survive being pasted into a tweet, a chat message or a mail client,
// all of which truncate. /s also sits outside the PROTECTED prefixes in
// middleware.ts, which is what lets a signed-out visitor open it at all.
export function shareHref(token: string): string {
  return `/s/${encodeURIComponent(token)}`;
}

// The absolute form, for copying and for sending somewhere else.
//
// Built from the browser's own origin rather than an env var, so a link copied
// from a preview deployment points at that preview and one copied from
// production points at production. Falls back to the relative path when there
// is no window (SSR), where an absolute URL would be a guess.
export function shareUrl(token: string): string {
  const path = shareHref(token);
  if (typeof window === "undefined") return path;
  return `${window.location.origin}${path}`;
}

/**
 * A `next` target that is safe to navigate to after sign-in, or null.
 *
 * Only a path on this app: it must start with "/" but not "//", which a
 * browser reads as another host, and must not contain a backslash, which
 * browsers normalize to "/" so "/\evil.com" would pass as a path and land off
 * site. Nor may it contain a control character: URL parsing silently drops
 * tabs and line breaks, so "/\n/evil.test" also becomes "//evil.test".
 *
 * Those checks are on the text; the last word goes to the parser that will
 * follow it. The target is resolved the way the router resolves it and must
 * stay on the same origin, so a spelling nobody listed here still cannot
 * leave. A sign-in page is refused too, judged by where the path resolves, so
 * a failed sign-in cannot loop back to itself. Shared by password sign-in and
 * the app's social sign-in.
 */
const RESOLVE_BASE = "https://app.invalid";

export function safeNextPath(raw: string | null | undefined): string | null {
  if (
    !raw ||
    !raw.startsWith("/") ||
    raw.startsWith("//") ||
    raw.includes("\\") ||
    /[\u0000-\u001f\u007f]/.test(raw)
  ) {
    return null;
  }
  let resolved: URL;
  try {
    resolved = new URL(raw, RESOLVE_BASE);
  } catch {
    return null;
  }
  if (
    resolved.origin !== RESOLVE_BASE ||
    resolved.pathname.startsWith("/signin") ||
    resolved.pathname.startsWith("/signup")
  ) {
    return null;
  }
  return raw;
}
