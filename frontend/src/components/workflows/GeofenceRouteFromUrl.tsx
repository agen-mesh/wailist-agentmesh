"use client";
import { useSearchParams } from "next/navigation";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { GeofenceScreen } from "./GeofenceScreen";

// Which workflow's zone this page is editing, resolved from the URL.
//
// The same problem WorkflowRouteFromUrl solves, and solved the same way on
// purpose. The native shell ships a static export, which can only contain
// pages that existed at build time, so it cannot prerender
// /workflows/<id>/geofence for ids belonging to users who have not signed up
// yet. The mobile build emits one shell page and the real id travels as ?id=.
//
// This matters more here than for the canvas: the geofence screen is primarily
// a phone screen, so the static-export path carries almost all of its traffic
// rather than being an edge case.
//
// Read in a client component because a static export prerenders the server
// component once, with no request and therefore no query string.
export function GeofenceRouteFromUrl({ routeId }: { routeId: string }) {
  const fromQuery = useSearchParams().get("id");
  // Only the native shell's ?id= may override the route segment. On the web
  // the path already carries the real id, and letting a query param win would
  // mean /workflows/<a>/geofence?id=<b> quietly arms a zone on a workflow the
  // URL does not name -- worse here than for a read-only view, since a trigger
  // spends money every time it fires.
  const workflowId = IS_NATIVE && fromQuery ? fromQuery : routeId;
  return <GeofenceScreen key={workflowId} workflowId={workflowId} />;
}
