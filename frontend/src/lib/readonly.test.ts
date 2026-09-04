import { describe, it, expect } from "vitest";
import { can, isWriteBlocked } from "./readonly";

// The two modes the app ships in: a desktop editor and a small-screen viewer.
const EDITOR = false;
const VIEWER = true;

describe("can", () => {
  it("withholds authoring from a viewer", () => {
    expect(can("workflow.create", VIEWER)).toBe(false);
    expect(can("workflow.delete", VIEWER)).toBe(false);
    expect(can("workflow.editGraph", VIEWER)).toBe(false);
    expect(can("workflow.deploy", VIEWER)).toBe(false);
    expect(can("workflow.buildFromChat", VIEWER)).toBe(false);
  });

  it("lets a viewer operate a workflow somebody else built", () => {
    expect(can("workflow.run", VIEWER)).toBe(true);
    expect(can("workflow.stop", VIEWER)).toBe(true);
    expect(can("workflow.chat", VIEWER)).toBe(true);
  });

  // The one authoring-shaped action a viewer keeps, and the reason the Android
  // app has anything to trigger. Asserted next to the withholding test above
  // so the two read as one decision rather than an inconsistency.
  it("lets a viewer configure a geofence, and only a geofence", () => {
    expect(can("workflow.geofence", VIEWER)).toBe(true);
    // The neighbours it is most easily confused with stay shut. Setting a
    // trigger is not editing the thing it triggers.
    expect(can("workflow.editGraph", VIEWER)).toBe(false);
    expect(can("workflow.deploy", VIEWER)).toBe(false);
    expect(can("workflow.create", VIEWER)).toBe(false);
  });

  it("leaves a viewer's account alone", () => {
    expect(can("account.billing", VIEWER)).toBe(true);
    expect(can("account.settings", VIEWER)).toBe(true);
  });

  // The whole point of the change: a desktop client is not restricted at all.
  it("withholds nothing from an editor", () => {
    const every = [
      "workflow.create",
      "workflow.delete",
      "workflow.editGraph",
      "workflow.deploy",
      "workflow.buildFromChat",
      "workflow.geofence",
      "workflow.run",
      "workflow.stop",
      "workflow.chat",
      "account.billing",
      "account.settings",
    ] as const;
    for (const c of every) expect(can(c, EDITOR)).toBe(true);
  });
});

describe("isWriteBlocked", () => {
  it("blocks the five graph-mutating endpoints for a viewer", () => {
    expect(isWriteBlocked("POST", "/workflows", VIEWER)).toBe(true);
    expect(isWriteBlocked("PUT", "/workflows/wf_123", VIEWER)).toBe(true);
    expect(isWriteBlocked("DELETE", "/workflows/wf_123", VIEWER)).toBe(true);
    expect(isWriteBlocked("POST", "/workflows/wf_123/deploy", VIEWER)).toBe(
      true,
    );
    expect(isWriteBlocked("POST", "/workflows/wf_123/build", VIEWER)).toBe(
      true,
    );
  });

  // Added alongside PUT/DELETE .../schedule and GET /tendril/console -- these
  // must mirror backend/internal/api/readonly.go one for one, same as every
  // other rule in this list.
  it("blocks schedule and the tendril console for a viewer", () => {
    expect(isWriteBlocked("PUT", "/workflows/wf_123/schedule", VIEWER)).toBe(
      true,
    );
    expect(
      isWriteBlocked("DELETE", "/workflows/wf_123/schedule", VIEWER),
    ).toBe(true);
    // GET, not a write verb -- listed because the backend handler behind it
    // creates a workflow row on first call (get-or-create), so it is a write
    // in effect. See lib/tendril.ts's console().
    expect(isWriteBlocked("GET", "/tendril/console", VIEWER)).toBe(true);
  });

  // The geofence exception, pinned from the guard's side.
  //
  // This is the half that actually ships the decision: the capability above
  // permits it, and this is what stops the fetch guard from throwing before
  // the request is built. Both halves have to hold, so both are tested --
  // either one reverting alone leaves a control that looks available and is
  // not.
  it("lets a viewer configure a geofence", () => {
    expect(isWriteBlocked("PUT", "/workflows/wf_123/geofence", VIEWER)).toBe(
      false,
    );
    expect(
      isWriteBlocked("DELETE", "/workflows/wf_123/geofence", VIEWER),
    ).toBe(false);
  });

  // Guards the blast radius rather than the feature: the geofence rules were
  // removed by deleting two entries from a list, and deleting one line too
  // many there would quietly open workflow deletion to every phone in the
  // world. The neighbours are asserted so that mistake fails a test instead
  // of shipping.
  it("still blocks the neighbouring rules the geofence entries sat between", () => {
    expect(isWriteBlocked("PUT", "/workflows/wf_123", VIEWER)).toBe(true);
    expect(isWriteBlocked("DELETE", "/workflows/wf_123", VIEWER)).toBe(true);
    expect(isWriteBlocked("PUT", "/workflows/wf_123/schedule", VIEWER)).toBe(
      true,
    );
    expect(isWriteBlocked("GET", "/tendril/console", VIEWER)).toBe(true);
  });

  it("blocks nothing for an editor", () => {
    expect(isWriteBlocked("POST", "/workflows", EDITOR)).toBe(false);
    expect(isWriteBlocked("DELETE", "/workflows/wf_123", EDITOR)).toBe(false);
    expect(isWriteBlocked("POST", "/workflows/wf_1/deploy", EDITOR)).toBe(
      false,
    );
  });

  it("lets a run, a stop, and every read through", () => {
    expect(isWriteBlocked("POST", "/workflows/wf_123/run", VIEWER)).toBe(false);
    expect(isWriteBlocked("POST", "/workflows/wf_123/stop", VIEWER)).toBe(
      false,
    );
    expect(isWriteBlocked("GET", "/workflows", VIEWER)).toBe(false);
    expect(isWriteBlocked("GET", "/workflows/wf_123", VIEWER)).toBe(false);
    expect(isWriteBlocked("POST", "/credits/redeem-coupon", VIEWER)).toBe(
      false,
    );
    expect(isWriteBlocked("PATCH", "/settings", VIEWER)).toBe(false);
    expect(isWriteBlocked("POST", "/auth/password", VIEWER)).toBe(false);
  });

  it("is case-insensitive on the method", () => {
    expect(isWriteBlocked("post", "/workflows", VIEWER)).toBe(true);
    expect(isWriteBlocked("delete", "/workflows/wf_1", VIEWER)).toBe(true);
  });

  // A blocked path must stay blocked however it is dressed up, or the guard is
  // decorative -- these are the shapes a fetch helper can produce by accident.
  it("cannot be slipped past with a query string or trailing slash", () => {
    expect(isWriteBlocked("POST", "/workflows?draft=1", VIEWER)).toBe(true);
    expect(isWriteBlocked("POST", "/workflows/", VIEWER)).toBe(true);
    expect(isWriteBlocked("PUT", "/workflows/wf_1/", VIEWER)).toBe(true);
    expect(
      isWriteBlocked("POST", "/workflows/wf_1/deploy?force=1", VIEWER),
    ).toBe(true);
  });

  // The id segment is one path element. A deeper path is a different endpoint
  // and must not be swept up by the single-segment rules.
  it("does not over-match nested routes", () => {
    expect(isWriteBlocked("PUT", "/workflows/wf_1/agents/a_1", VIEWER)).toBe(
      false,
    );
    expect(
      isWriteBlocked("POST", "/workflows/wf_1/agents/a_1/fund", VIEWER),
    ).toBe(false);
  });
});
