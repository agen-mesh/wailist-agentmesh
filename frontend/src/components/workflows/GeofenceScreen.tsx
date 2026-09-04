"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { workflows } from "@/lib/api";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { can } from "@/lib/readonly";
import { useReadOnly } from "@/hooks/useReadOnly";
import {
  MAX_RADIUS_M,
  MIN_RADIUS_M,
  accuracyTooVague,
  checkRadius,
  distanceM,
  formatCoords,
  formatDistance,
  radiusToSlider,
  sliderToRadius,
  type Point,
} from "@/lib/geofence";
import { Card } from "@/components/ui";
// ghostBtn/primaryBtn are not re-exported by the ui barrel (only
// ghostBtnSm is), so they come from the module that owns every button
// style and the contract that keeps a label inside its own box.
import { ghostBtn, primaryBtn } from "@/components/ui/buttons";
import type { Workflow } from "@/lib/types";

// Where a workflow's location trigger is chosen.
//
// A screen of its own, not a node on the canvas. #106 and #112 both say so,
// and the reason is the same one that makes this the single authoring-shaped
// thing a phone may do: you pick a zone by standing in it. A canvas node would
// put the control on the device furthest from the place it describes.
//
// Deliberately keyless -- coordinates, a radius, and a distance readout, with
// no map tile behind them. A map would mean a Google Maps key to hold and bill,
// or an OSM tile server to attribute and stay polite to, bought in exchange for
// prettiness on a decision the numbers already answer: am I in the right place,
// and is the circle the right size.

type Status =
  | { kind: "idle" }
  | { kind: "locating" }
  | { kind: "saving" }
  | { kind: "clearing" }
  | { kind: "error"; message: string }
  | { kind: "saved" };

// The two refusal states `requestBackgroundLocation` (native/permissions.ts)
// can leave a screen in. Kept separate from Status: whether the server has a
// fence and whether the OS is watching it are two different facts, and a
// permission refusal must not read as a save that failed.
type PermissionState = "denied" | "denied-permanently";

// Long enough to get a real fix rather than the last cached one, short enough
// that a phone indoors with no sky in view gives up and says so.
const FIX_TIMEOUT_MS = 15_000;

export function GeofenceScreen({ workflowId }: { workflowId: string }) {
  const readOnly = useReadOnly();
  const allowed = can("workflow.geofence", readOnly);

  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [here, setHere] = useState<Point | null>(null);
  const [accuracyM, setAccuracyM] = useState<number | null>(null);
  const [radiusM, setRadiusM] = useState<number>(150);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  // Whether the OS is actually watching the zone just saved. null on web
  // (the question does not apply there) and before the first native save in
  // this session -- shell.setGeofence is the only thing that learns the
  // answer, so there is nothing honest to show before it has run once.
  const [deviceArmed, setDeviceArmed] = useState<boolean | null>(null);
  const [permission, setPermission] = useState<PermissionState | null>(null);

  // The zone as the server currently has it, or null if there is none. All
  // three fields move together -- a partly configured zone means nothing.
  const saved = useMemo<{ centre: Point; radiusM: number } | null>(() => {
    if (
      !workflow ||
      workflow.geofenceLat === undefined ||
      workflow.geofenceLng === undefined ||
      workflow.geofenceRadiusM === undefined
    ) {
      return null;
    }
    return {
      centre: { lat: workflow.geofenceLat, lng: workflow.geofenceLng },
      radiusM: workflow.geofenceRadiusM,
    };
  }, [workflow]);

  useEffect(() => {
    let stale = false;
    workflows
      .get(workflowId)
      .then((w) => {
        if (stale) return;
        setWorkflow(w);
        if (w.geofenceRadiusM !== undefined) setRadiusM(w.geofenceRadiusM);
      })
      .catch((err: unknown) => {
        if (!stale) {
          setLoadError(
            err instanceof Error ? err.message : "could not load this workflow",
          );
        }
      });
    return () => {
      stale = true;
    };
  }, [workflowId]);

  // Held so a fix that arrives after the user has navigated away cannot set
  // state on an unmounted screen. getCurrentPosition has no abort handle, so
  // the callback has to check for itself.
  const liveRef = useRef(true);
  useEffect(() => {
    liveRef.current = true;
    return () => {
      liveRef.current = false;
    };
  }, []);

  const locate = useCallback(() => {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      setStatus({
        kind: "error",
        message: "This device cannot report its location.",
      });
      return;
    }
    setStatus({ kind: "locating" });
    // The Web Geolocation API rather than a new native method: inside the
    // Android shell the WebView is an https origin (capacitor.config.ts sets
    // androidScheme), so this is a secure context, and Capacitor's
    // BridgeWebChromeClient forwards the prompt to the same OS permission the
    // geofence flow already asks for. No new native surface for a reading the
    // platform already gives us -- and the screen still works in a desktop
    // browser, where it can be developed and looked at.
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        if (!liveRef.current) return;
        setHere({ lat: pos.coords.latitude, lng: pos.coords.longitude });
        setAccuracyM(pos.coords.accuracy ?? null);
        setStatus({ kind: "idle" });
      },
      (err) => {
        if (!liveRef.current) return;
        setStatus({
          kind: "error",
          message:
            err.code === err.PERMISSION_DENIED
              ? "Location access is off for AgentMesh. Turn it on in Settings to pick a zone."
              : "Could not get a location fix. Try again with a clearer view of the sky.",
        });
      },
      { enableHighAccuracy: true, timeout: FIX_TIMEOUT_MS, maximumAge: 0 },
    );
  }, []);

  const radiusCheck = checkRadius(radiusM);

  const save = useCallback(async () => {
    if (!here) return;
    const check = checkRadius(radiusM);
    if (!check.ok) {
      setStatus({ kind: "error", message: check.message });
      return;
    }
    // A fix carries its own error bar, and this screen was showing it without
    // acting on it. When the bar is wider than the zone, the centre can sit
    // entirely outside the circle the user believes they drew. Refused rather
    // than warned: widening the zone past the accuracy is always available,
    // and a misplaced zone does not fail visibly, it fires in the wrong place.
    if (accuracyTooVague(accuracyM, check.radiusM)) {
      setStatus({
        kind: "error",
        message: `This fix is only accurate to about ${formatDistance(
          accuracyM,
        )}, which is wider than the ${formatDistance(
          check.radiusM,
        )} zone. Widen the zone, or find a clearer view of the sky and locate again.`,
      });
      return;
    }
    setStatus({ kind: "saving" });
    // Stale until the native path below learns otherwise. A save that fails
    // outright leaves this at whatever a previous session last reported,
    // which is fine -- the error notice takes over the message either way.
    setDeviceArmed(null);
    const fence = { lat: here.lat, lng: here.lng, radiusM: check.radiusM };
    try {
      if (IS_NATIVE) {
        // The shell's path: it writes to the server first and only then arms
        // Android's GeofencingClient, so a rejected zone never leaves the
        // device watching a boundary the server will not act on.
        //
        // The return is whether the OS actually armed the fence -- false
        // means background location was never granted (permissions.ts's
        // requestBackgroundLocation is never called anywhere else in this
        // app, so that is the common case for a first save). Discarding it
        // would report success on a trigger that will never fire.
        const { shell } = await import("@/native");
        const armed = await shell.setGeofence(workflowId, fence);
        if (!liveRef.current) return;
        setDeviceArmed(armed);
        if (armed) setPermission(null);
      } else {
        await workflows.setGeofence(workflowId, fence);
      }
      if (!liveRef.current) return;
      setWorkflow((w) =>
        w
          ? {
              ...w,
              geofenceLat: fence.lat,
              geofenceLng: fence.lng,
              geofenceRadiusM: fence.radiusM,
              // Saving resets the recorded state server-side --
              // SetWorkflowGeofence nulls geofence_inside and
              // geofence_last_fix_at -- so the next fix re-baselines instead
              // of reading as a crossing that never happened. Cleared here
              // too, or the screen would keep showing an "inside" belonging to
              // the zone that was just replaced.
              geofenceInside: undefined,
              geofenceLastFixAt: undefined,
            }
          : w,
      );
      setStatus({ kind: "saved" });
    } catch (err: unknown) {
      if (!liveRef.current) return;
      setStatus({
        kind: "error",
        message: err instanceof Error ? err.message : "could not save the zone",
      });
    }
  }, [accuracyM, here, radiusM, workflowId]);

  // Asks for background location and, if granted, re-arms the OS watch for
  // the zone the server already has. The server write from the save above
  // already succeeded -- shell.setGeofence's PUT is idempotent -- so this
  // only needs to redo the native half.
  //
  // Not exposed on web: requestBackgroundLocation lives in native/permissions
  // and calls a plugin that only exists inside the Capacitor shell.
  const requestAccess = useCallback(async () => {
    if (!IS_NATIVE) return;
    const perms = await import("@/native/permissions");
    if (permission === "denied-permanently") {
      // Android will not show the system dialog again after an outright
      // refusal -- Settings is the only way back.
      await perms.openSettings();
      return;
    }
    const result = await perms.requestBackgroundLocation();
    if (!liveRef.current) return;
    if (result !== "granted") {
      setPermission(result);
      return;
    }
    setPermission(null);
    if (!saved) return;
    const { shell } = await import("@/native");
    const armed = await shell.setGeofence(workflowId, {
      lat: saved.centre.lat,
      lng: saved.centre.lng,
      radiusM: saved.radiusM,
    });
    if (liveRef.current) setDeviceArmed(armed);
  }, [permission, saved, workflowId]);

  const clear = useCallback(async () => {
    setStatus({ kind: "clearing" });
    try {
      if (IS_NATIVE) {
        const { shell } = await import("@/native");
        await shell.clearGeofence(workflowId);
      } else {
        await workflows.clearGeofence(workflowId);
      }
      if (!liveRef.current) return;
      setWorkflow((w) =>
        w
          ? {
              ...w,
              geofenceLat: undefined,
              geofenceLng: undefined,
              geofenceRadiusM: undefined,
              geofenceInside: undefined,
              geofenceLastFixAt: undefined,
            }
          : w,
      );
      setStatus({ kind: "idle" });
      // Nothing left to be armed or refused about once the zone is gone.
      setDeviceArmed(null);
      setPermission(null);
    } catch (err: unknown) {
      if (!liveRef.current) return;
      setStatus({
        kind: "error",
        message:
          err instanceof Error ? err.message : "could not remove the zone",
      });
    }
  }, [workflowId]);

  // How far the current reading is from the zone already saved. The one number
  // that answers "is the circle where I think it is" without a map.
  const distanceToSaved = here && saved ? distanceM(saved.centre, here) : null;

  const busy =
    status.kind === "saving" ||
    status.kind === "clearing" ||
    status.kind === "locating";
  // workflow !== null, not merely "no load error": until the fetch returns
  // there is no way to know whether this workflow exists, belongs to this
  // user, or is deployed, and a PUT sent on that ignorance can only produce an
  // error the user cannot act on. The failed load is the case that matters --
  // it leaves this null for good, and the form was previously fully live
  // behind the error notice, offering to save into a workflow it could not
  // read.
  const canSave = Boolean(here) && workflow !== null && !busy && radiusCheck.ok;

  if (!allowed) {
    // Unreachable as things stand -- "workflow.geofence" is permitted for
    // viewers and editors alike. Kept because the capability is the thing that
    // decides, and a screen that assumed its own answer would quietly ignore
    // the policy the day it changes.
    return (
      <Wrap>
        <p style={copy}>Location triggers are not available on this device.</p>
      </Wrap>
    );
  }

  return (
    <Wrap>
      <header style={{ marginBottom: 20 }}>
        <h1 style={title}>Location trigger</h1>
        <p style={{ ...copy, marginTop: 6 }}>
          {workflow?.name
            ? `Start ${workflow.name} when you arrive at or leave a place.`
            : "Start this workflow when you arrive at or leave a place."}
        </p>
      </header>

      {loadError && <Notice tone="danger">{loadError}</Notice>}

      <Card style={{ marginBottom: 12 }}>
        <SectionLabel>Where</SectionLabel>
        {here ? (
          <>
            <p style={mono}>{formatCoords(here)}</p>
            {accuracyM !== null && (
              <p style={{ ...copy, marginTop: 4 }}>
                Accurate to about {formatDistance(accuracyM)}.
              </p>
            )}
            {/* Said next to the fix it describes rather than only on save:
                the fix is the thing that is wrong, and the user can act on it
                (locate again, or widen the zone) before reaching the button. */}
            {accuracyTooVague(accuracyM, radiusM) && (
              <Notice tone="danger">
                This fix is vaguer than the zone is wide, so the centre could be
                anywhere within {formatDistance(accuracyM)} of here. Widen the
                zone, or locate again with a clearer view of the sky.
              </Notice>
            )}
          </>
        ) : (
          <p style={copy}>
            The zone is centred on where you are standing when you save it.
          </p>
        )}
        <button
          type="button"
          onClick={locate}
          disabled={busy}
          style={{ ...ghostBtn, ...touchTarget, marginTop: 12 }}
        >
          {status.kind === "locating"
            ? "Finding you"
            : here
              ? "Update to where I am now"
              : "Use my location"}
        </button>
      </Card>

      <Card style={{ marginBottom: 12 }}>
        <SectionLabel>How big</SectionLabel>
        <p style={{ ...mono, fontSize: 22 }}>{formatDistance(radiusM)}</p>
        <input
          type="range"
          min={0}
          max={1000}
          value={Math.round(radiusToSlider(radiusM) * 1000)}
          onChange={(e) =>
            setRadiusM(sliderToRadius(Number(e.target.value) / 1000))
          }
          disabled={busy}
          aria-label="Zone radius"
          style={slider}
        />
        <div style={scaleRow}>
          <span>{formatDistance(MIN_RADIUS_M)}</span>
          <span>{formatDistance(MAX_RADIUS_M)}</span>
        </div>
        {!radiusCheck.ok && (
          <Notice tone="danger">{radiusCheck.message}</Notice>
        )}
      </Card>

      {saved && (
        <Card style={{ marginBottom: 12 }}>
          <SectionLabel>Saved zone</SectionLabel>
          <p style={mono}>{formatCoords(saved.centre)}</p>
          <p style={{ ...copy, marginTop: 4 }}>
            Radius {formatDistance(saved.radiusM)}.
            {distanceToSaved !== null && (
              <> You are {formatDistance(distanceToSaved)} from its centre.</>
            )}
          </p>
        </Card>
      )}

      {!IS_NATIVE && (
        <Notice tone="info">
          Crossings are noticed by the AgentMesh app on your Android phone.
          Saving a zone here records it; the phone is what watches the edge.
        </Notice>
      )}

      {status.kind === "error" && (
        <Notice tone="danger">{status.message}</Notice>
      )}
      {/* Two different facts get two different notices: the server can hold
          a fence the phone is not watching, and telling the user "saved"
          when nothing will ever fire is the exact bug this screen exists to
          not have. deviceArmed is only ever false here on native -- the web
          branch of save() never touches it, so it stays at its initial
          null. */}
      {status.kind === "saved" && deviceArmed === false && (
        <Notice tone="danger">
          Zone saved, but this phone is not watching it yet.{" "}
          {permission === "denied-permanently"
            ? "Location access was refused, and Android will not ask again from inside the app."
            : "AgentMesh needs “Allow all the time” location access to notice a crossing while it's closed."}
          <div style={{ marginTop: 8 }}>
            <button
              type="button"
              onClick={requestAccess}
              style={{ ...ghostBtn, ...touchTarget }}
            >
              {permission === "denied-permanently"
                ? "Open Settings"
                : "Turn on location access"}
            </button>
          </div>
        </Notice>
      )}
      {/* A save from a phone's web browser is not the same event as a save
          from the app and must not wear the same green notice. The server
          stores the zone and then waits for position reports a plain browser
          never sends: no GeofencingClient, and nothing running once the tab
          closes. Saying "saved" here, beside copy about the next reading,
          promises a reading that is not coming. */}
      {status.kind === "saved" && !IS_NATIVE && (
        <Notice tone="info">
          Zone recorded, but nothing is watching it yet. Crossings are noticed
          by the AgentMesh app on your Android phone; install it and sign in,
          and this zone starts working with no further setup.
        </Notice>
      )}
      {status.kind === "saved" && IS_NATIVE && deviceArmed !== false && (
        <Notice tone="ok">
          Zone saved. The next reading sets the starting point, so being here
          right now will not count as an arrival you did not make.
        </Notice>
      )}

      <div style={actionRow}>
        <button
          type="button"
          onClick={save}
          disabled={!canSave}
          style={{
            ...primaryBtn,
            ...touchTarget,
            opacity: canSave ? 1 : 0.5,
            cursor: canSave ? "pointer" : "not-allowed",
          }}
        >
          {status.kind === "saving"
            ? "Saving"
            : saved
              ? "Move zone here"
              : "Save zone"}
        </button>
        {saved && (
          <button
            type="button"
            onClick={clear}
            disabled={busy}
            style={{ ...ghostBtn, ...touchTarget }}
          >
            {status.kind === "clearing" ? "Removing" : "Remove zone"}
          </button>
        )}
      </div>
    </Wrap>
  );
}

// -- Local presentation -----------------------------------------------------

const title: React.CSSProperties = {
  font: "600 20px/1.3 var(--font-sans)",
  color: "var(--fg)",
  margin: 0,
};

// Body copy is capped near 65 characters. A location disclosure that runs the
// full width of a desktop window is one nobody finishes reading.
const copy: React.CSSProperties = {
  font: "400 13px/1.6 var(--font-sans)",
  color: "var(--fg-muted)",
  maxWidth: "60ch",
  margin: 0,
};

// Coordinates and distances are figures that change in place as a fix updates.
// Tabular so the line does not jiggle left and right on every reading.
const mono: React.CSSProperties = {
  font: "500 15px/1.4 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  color: "var(--fg)",
  margin: 0,
};

const slider: React.CSSProperties = {
  width: "100%",
  marginTop: 8,
  accentColor: "var(--accent)",
};

const scaleRow: React.CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  font: "400 11px/1 var(--font-mono)",
  fontVariantNumeric: "tabular-nums",
  color: "var(--fg-dim)",
  marginTop: 6,
};

const actionRow: React.CSSProperties = {
  display: "flex",
  gap: 8,
  flexWrap: "wrap",
  marginTop: 16,
};

// Every button here is pressed with a thumb, so they clear 44px rather than
// inheriting a density meant for a desktop toolbar.
const touchTarget: React.CSSProperties = { minHeight: 44 };

function Wrap({ children }: { children: React.ReactNode }) {
  return (
    <main style={page}>
      <div style={{ maxWidth: 560, margin: "0 auto" }}>{children}</div>
    </main>
  );
}

const page: React.CSSProperties = {
  minHeight: "100dvh",
  background: "var(--bg)",
  padding: "24px 16px 40px",
};

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <h2 style={sectionLabel}>{children}</h2>;
}

const sectionLabel: React.CSSProperties = {
  font: "500 11px/1 var(--font-mono)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  color: "var(--fg-dim)",
  margin: "0 0 10px",
};

// Reuses the .reveal entrance from globals.css rather than defining another
// keyframe, which also picks up its prefers-reduced-motion escape for free.
function Notice({
  tone,
  children,
}: {
  tone: "danger" | "info" | "ok";
  children: React.ReactNode;
}) {
  const color =
    tone === "danger"
      ? "var(--danger)"
      : tone === "ok"
        ? "var(--accent)"
        : "var(--info)";
  return (
    <div className="reveal" style={{ ...notice, borderColor: color }}>
      {children}
    </div>
  );
}

const notice: React.CSSProperties = {
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  padding: "10px 12px",
  marginTop: 12,
  font: "400 12px/1.6 var(--font-sans)",
  color: "var(--fg)",
  maxWidth: "60ch",
};
