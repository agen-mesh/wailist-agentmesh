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
