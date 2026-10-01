import { isHandheldNow } from "./device";
import { IS_NATIVE } from "./nativeAuth";

// The web app is an editor on a computer and a viewer on a handheld; the
// native app is a viewer on every device.
//
// Browse anywhere, author where you have the keyboard and pointer to do it
// with. The line is drawn by DEVICE, not by window width -- a laptop dragged
// narrow is still a laptop, and taking its editor away because of a window
// size was simply a bug. See lib/device.ts for how a handheld is identified
// and why no single browser API suffices.

// Named for what the user is trying to do, not for the endpoint behind it, so
// a call site reads as intent: `can("workflow.deploy", readOnly)`.
export type Capability =
  // Authoring — needs the room a desktop has.
  | "workflow.create"
  | "workflow.delete"
  | "workflow.editGraph"
  | "workflow.deploy"
  | "workflow.buildFromChat"
  // Configuring a trigger from where you are. Not authoring, though it is
  // shaped like it -- see WITHHELD below for why this one is permitted.
  | "workflow.geofence"
  // Operating — available on any screen.
  | "workflow.run"
  | "workflow.stop"
  | "workflow.chat"
  | "account.billing"
  | "account.settings";

// Everything absent from this set stays available. Listing what is *withheld*
// (rather than what is permitted) means a capability added later is readable
// by default, and has to be denied on purpose.
const WITHHELD: ReadonlySet<Capability> = new Set<Capability>([
  "workflow.create",
  "workflow.delete",
  "workflow.editGraph",
  "workflow.deploy",
  // Build mode rewrites the graph from a chat message, so it is authoring
  // wearing a conversation's clothes. Ordinary chat with an already-deployed
  // workflow is not, and stays open.
  "workflow.buildFromChat",
]);

// "workflow.geofence" is deliberately ABSENT from that set, and it is the one
// authoring-shaped thing a viewer may do.
//
// The rest of the list withholds actions that need a keyboard, a pointer and
// room -- none of which a phone has. Choosing where a fence sits needs none of
// them; it needs you to be standing in the place. A geofence set from a desk,
// from a map you are not looking at, is the version that is hard to get right.
// So the device that is worst at authoring is the one best placed to do this,
// and withholding it would mean the trigger could only ever be configured
// somewhere its author cannot see what they are configuring.
//
// It is also the whole point of the Android app (#112): a viewer that cannot
// set a fence has nothing to trigger. Nothing else moves -- a viewer still
// cannot create, delete, deploy or edit a graph.

// Pure on purpose: `readOnly` is passed in rather than measured here, so the
// policy can be tested without a DOM and so React components drive it from
// useReadOnly() -- a function that measured the viewport itself would not
// re-render anything when the window crossed the breakpoint.
export function can(capability: Capability, readOnly: boolean): boolean {
  if (!readOnly) return true;
  return !WITHHELD.has(capability);
}

// Defence in depth for the API layer. Hiding a control is a UX decision; this
// is the guarantee that a missed control, a stale bundle, or a deep link
// cannot still put a write on the wire from a viewer.
//
// The rules deliberately mirror backend/internal/api/readonly.go one for one.
const WRITE_RULES: ReadonlyArray<{ method: string; pattern: RegExp }> = [
  { method: "POST", pattern: /^\/workflows$/ },
  { method: "PUT", pattern: /^\/workflows\/[^/]+$/ },
  { method: "DELETE", pattern: /^\/workflows\/[^/]+$/ },
  { method: "POST", pattern: /^\/workflows\/[^/]+\/deploy$/ },
  { method: "POST", pattern: /^\/workflows\/[^/]+\/build$/ },
  { method: "PUT", pattern: /^\/workflows\/[^/]+\/schedule$/ },
  { method: "DELETE", pattern: /^\/workflows\/[^/]+\/schedule$/ },
  // Variables are values a workflow's nodes read, so writing one is authoring.
  // Listing them stays open.
  { method: "PUT", pattern: /^\/workflows\/[^/]+\/variables\/[^/]+$/ },
  { method: "DELETE", pattern: /^\/workflows\/[^/]+\/variables\/[^/]+$/ },
  // PUT/DELETE .../geofence used to sit here. They were removed with the
  // capability above: leaving them would have made the guard contradict the
  // policy, so a viewer would see the control, press it, and get an exception
  // instead of a saved zone.
  { method: "GET", pattern: /^\/tendril\/console$/ },
  // Same exception, same reason: a GET that find-or-creates the console's
  // workflow row. /prism/console/exists never creates one, and /prism/run is
  // operating rather than authoring, so neither is listed -- exactly the line
  // /tendril/console/exists and /tendril/run already sit on.
  { method: "GET", pattern: /^\/prism\/console$/ },
  // And the HelixBox console, which the backend list already carries.
  { method: "GET", pattern: /^\/helixbox\/console$/ },
];

// `path` is the API path as written at the call site (leading slash, no
// origin, no query). Query strings and trailing slashes are normalised off
// first so a caller cannot slip a write past by appending either.
export function isWriteBlocked(
  method: string,
  path: string,
  readOnly: boolean,
): boolean {
  if (!readOnly) return false;
  const m = method.toUpperCase();
  let p = path.split("?")[0].split("#")[0];
  if (p.length > 1 && p.endsWith("/")) p = p.slice(0, -1);
  return WRITE_RULES.some((r) => r.method === m && r.pattern.test(p));
}

// For callers outside React, which is really just the fetch guards below.
// A one-shot reading is the right shape there: it answers "may this call go
// out, right now", and nothing needs to re-render when the answer changes.
// Components must use useReadOnly() instead.
export function isReadOnlyNow(): boolean {
  return IS_NATIVE || isHandheldNow();
}

// Defence in depth for the API layer, called by lib/api.ts before each write it
// sends from the WEB bundle. Not for native/api.ts: that file runs only inside
// the native Android shell, which isReadOnlyNow() treats as read-only
// unconditionally -- gating it on this same check would block every native
// write permanently rather than just a web viewer's.
export function assertWritable(method: string, path: string): void {
  if (isWriteBlocked(method, path, isReadOnlyNow())) {
    throw new Error(
      "Workflows can only be edited in the AgentMesh desktop app.",
    );
  }
}
