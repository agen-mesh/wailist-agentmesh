// Push notifications: asking, registering, and what a tap does.
//
// The device half of #132. The server decides WHICH runs are worth a
// notification (backend/internal/push.ShouldNotify); this file only concerns
// itself with being reachable, and with what happens when one arrives.
//
// None of it works until a Firebase project exists and its
// google-services.json is in place -- without that the Android build does not
// include FCM at all, register() fails, and every function below reports it
// honestly rather than pretending. See mobile/README.md.
import type { PluginListenerHandle } from "@capacitor/core";
import { PushNotifications } from "@capacitor/push-notifications";
import { registerDevice, unregisterDevice } from "./api";
import { clearOptedIn, hasOptedIn, setOptedIn } from "./pushPrefs";

// What the user is told BEFORE Android's own dialog, for the same reason
// permissions.ts explains background location first: a cold system prompt is
// refused far more often, and on Android 13+ notifications are a one-shot
// runtime permission like any other -- refuse once and the only way back is
// Settings.
//
// Shorter than the location disclosure on purpose. Notifications are a much
// smaller ask, and padding a small ask with a wall of text reads as though
// something is being hidden.
export const PUSH_DISCLOSURE = {
  title: "Tell me when a workflow finishes",
  body:
    "AgentMesh can notify you when a workflow you did not start finishes: " +
    "one triggered by arriving somewhere, or by a schedule, and whenever a " +
    "run fails.\n\n" +
    "Runs you start yourself do not notify, since you are already looking " +
    "at them.",
  grant: "Turn on notifications",
  decline: "Not now",
} as const;

export type PushState = "granted" | "denied" | "unavailable";

// What a screen can be told without asking for anything.
//
// "off" is the state enablePush() cannot report, because reaching that function
// means having already asked -- and on Android 13+ asking is a one-shot. It
// covers both "Android has never been asked" and "Android says yes but nobody
// here turned it on", which are the same thing to a reader: notifications are
// not arriving, and there is a way to start them.
export type PushReadState = PushState | "off";

// Long enough for a cold FCM registration on a slow connection, short enough
// that a device which will never register does not hold anything up.
const REGISTRATION_TIMEOUT_MS = 15_000;

// The token last registered with the server, kept so sign-out can say which
// row to drop.
//
// Deliberately in memory rather than persisted: it is only needed between a
// sign-in and the matching sign-out inside one app session. A stale token
// surviving a restart would be worse than useless, because FCM may have
// rotated it meanwhile and the server would be asked to delete a row that no
// longer describes this device.
let currentToken: string | null = null;
let generation = 0;
let disabled = false;
let enabling: { generation: number; promise: Promise<PushState> } | null = null;
let disabling: Promise<void> | null = null;
let pendingWrite: Promise<void> | null = null;
let cancelRegistration: (() => void) | null = null;

/**
 * Asks for permission, registers with FCM, and tells the server where to find
 * this device.
 *
 * Call only after PUSH_DISCLOSURE has been shown and the user has chosen to
 * continue. Resolves with what actually happened rather than throwing: being
 * refused is an answer, not an error, and every caller wants to carry on
 * either way.
 */
export function enablePush(): Promise<PushState> {
  const attempt = generation;
  if (enabling?.generation === attempt) return enabling.promise;
  const previousDisable = disabling;
  const promise = (async (): Promise<PushState> => {
    await previousDisable;
    if (attempt !== generation) return "unavailable";
    disabled = false;
    try {
      let perm = await PushNotifications.checkPermissions();
      if (attempt !== generation) return "unavailable";
      if (
        perm.receive === "prompt" ||
        perm.receive === "prompt-with-rationale"
      ) {
        perm = await PushNotifications.requestPermissions();
      }
      if (attempt !== generation) return "unavailable";
      if (perm.receive !== "granted") return "denied";
      const token = await registerForToken();
      if (attempt !== generation || token === null) return "unavailable";
      currentToken = token;
      const write = (async () => {
        await registerDevice(token);
        if (attempt === generation) await setOptedIn();
      })();
      pendingWrite = write;
      try {
        await write;
      } finally {
        if (pendingWrite === write) pendingWrite = null;
      }
      return attempt === generation ? "granted" : "unavailable";
    } catch (err) {
      console.error("push: could not enable notifications", err);
      return "unavailable";
    }
  })();
  enabling = { generation: attempt, promise };
  void promise.finally(() => {
    if (enabling?.promise === promise) enabling = null;
  });
  return promise;
}

// Capture cancellation before reading preferences, which also crosses the bridge.
export async function restorePush(): Promise<void> {
  const attempt = generation;
  if ((await hasOptedIn()) && attempt === generation) await enablePush();
}

/**
 * Whether notifications are on for this app, without asking for anything.
 *
 * The distinction this exists for: enablePush() answers by REQUESTING, and on
 * Android 13+ the permission dialog is a one-shot -- refuse it once and the
 * only route back is Settings. So a screen that wants to render its own state
 * cannot use enablePush() to find out what that state is; doing so would burn
 * the single ask just to draw a toggle.
 *
 * TWO facts, not one. Android's permission is necessary and not sufficient:
 * turning notifications off in this app unregisters the device and clears the
 * opt-in, but it does NOT revoke the OS permission -- nothing in an app can.
 * A version of this function that reported only what Android says answered
 * "granted" the instant after the user pressed Turn off, so the sheet snapped
 * straight back to its "on" panel, and a device that had opted out on an
 * earlier visit opened on that panel too. Both were reported in review on
 * #174; both were invisible to a four-state walkthrough that never set up
 * "permission granted, opted out" as a case.
 *
 * "off" therefore covers never-asked and opted-out alike, which is the same
 * thing to a reader. "unavailable" means the plugin could not answer at all --
 * a build with no google-services.json, or a device with no Play services.
 */
export async function notificationState(): Promise<PushReadState> {
  const attempt = generation;
  try {
    const { receive } = await PushNotifications.checkPermissions();
    // Denied first: a refusal is worth saying out loud whatever the opt-in
    // records, because the route back is Settings rather than this app, and
    // that is the one thing the reader needs to be told.
    if (receive === "denied") return "denied";
    if (receive !== "granted" || disabled) return "off";
    const optedIn = await hasOptedIn();
    return attempt === generation && !disabled && optedIn ? "granted" : "off";
  } catch (err) {
    console.error("push: could not read the notification permission", err);
    return "unavailable";
  }
}

/**
 * Stops this device receiving notifications, and tells the server so.
 *
 * Called on sign-out, and when the user turns notifications off. Never
 * throws: a device that cannot reach the network must still be able to sign
 * out, and the server is not left holding a dead row either way -- FCM
 * rejects sends to an unregistered token, and the send path drops the row on
 * that verdict.
 *
 * Note what this cannot do after a cold start: currentToken is null, so the
 * server keeps the row until FCM refuses a send to it. The opt-in flag is
 * still cleared, so nothing re-arms it, and the row is dropped on the first
 * send that would have gone to this device. Turning it off is therefore
 * immediate on the phone and eventual on the server.
 */
export function disablePush(): Promise<void> {
  generation++;
  disabled = true;
  cancelRegistration?.();
  const write = pendingWrite;
  const previousDisable = disabling;
  const promise = (async () => {
    // Cleared first, and not conditional on holding a token. A cold start
    // leaves currentToken null while the device is still registered
    // server-side, so gating this on having a token would leave the flag set on
    // exactly the devices that most need it cleared -- and boot() would then
    // re-arm what the user just turned off.
    await clearOptedIn();
    await previousDisable;
    // Keep the session available until a pending server write has settled.
    // New enables wait for this cleanup before registering again.
    // The enable path reports a rejected registration; cleanup still runs.
    await write?.catch(() => {});
    // Cleared a SECOND time, deliberately. The write awaited above can be an
    // in-flight setOptedIn from an enable that started before this disable did;
    // clearing only before that await would let it land afterwards and leave
    // the device opted in with the switch showing off.
    await clearOptedIn();
    const token = currentToken;
    currentToken = null;
    if (token) {
      await unregisterDevice(token).catch((err) => {
        console.error("push: could not unregister this device", err);
      });
    }
    // NOT removeAllListeners(). The tap listener is attached once by boot() and
    // is not part of any one session: removing it here left a sign-out followed
    // by a sign-in, with no restart in between, unable to route a tapped
    // notification anywhere until the next cold start. registerForToken removes
    // the two listeners it owns as soon as it settles, so there is nothing of
    // this function's to tidy up.
    await PushNotifications.unregister().catch(() => {});
  })();
  disabling = promise;
  void promise.finally(() => {
    if (disabling === promise) disabling = null;
  });
  return promise;
}

/**
 * Resolves with the FCM registration token, or null if registration failed.
 *
 * register() resolves as soon as the request is made -- the token arrives
 * later on the 'registration' event, and a failure on 'registrationError'.
 * Awaiting register() alone therefore tells you nothing, which is the single
 * easiest mistake to make with this plugin.
 */
function registerForToken(): Promise<string | null> {
  return new Promise((resolve) => {
    let settled = false;
    // Removed as soon as this call settles. The `settled` guard already made
    // a stale listener harmless, but harmless is not the same as gone: every
    // enable/disable cycle used to leave two more attached, and the process
    // outlives many of them.
    const handles: PluginListenerHandle[] = [];
    const drop = (h: PluginListenerHandle) => {
      void h.remove().catch(() => {});
    };
    // addListener resolves its handle asynchronously, so a fast registration
    // (or the timeout) can settle before the handle exists. Whoever loses that
    // race removes it -- otherwise the listener outlives the promise it
    // belongs to, which is the leak this is fixing.
    const track = (pending: Promise<PluginListenerHandle>) => {
      void pending
        .then((h) => {
          if (settled) drop(h);
          else handles.push(h);
        })
        .catch(() => {});
    };
    const finish = (value: string | null) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (cancelRegistration === cancel) cancelRegistration = null;
      handles.splice(0).forEach(drop);
      resolve(value);
    };

    // Neither event is guaranteed to arrive. A device with no Play services,
    // or one whose google-services.json was never added, can leave both
    // unfired -- and an unresolved promise here would hang sign-in behind a
    // notification the user never asked for.
    const cancel = () => finish(null);
    cancelRegistration = cancel;
    const timer = setTimeout(cancel, REGISTRATION_TIMEOUT_MS);

    track(
      PushNotifications.addListener("registration", (t) => finish(t.value)),
    );
    track(
      PushNotifications.addListener("registrationError", (err) => {
        console.error("push: FCM registration failed", err);
        finish(null);
      }),
    );
    void PushNotifications.register().catch((err) => {
      console.error("push: register() rejected", err);
      finish(null);
    });
  });
}

let tapListener: PluginListenerHandle | null = null;

/**
 * Routes a tapped notification to the run it is about.
 *
 * The payload carries workflowId as well as runId because the app has no route
 * for a run on its own -- a run is shown inside its workflow's page, so a tap
 * carrying only a run id would have nowhere to go.
 *
 * Navigation is a full location assignment rather than a router push: the tap
 * can arrive when the app was not running at all, in which case there is no
 * router mounted yet to push onto.
 */
export async function listenForTaps(): Promise<void> {
  // Idempotent. boot() is the only caller today, but a second attachment
  // would route one tap twice, and this is now the listener that has to
  // survive a whole process rather than a single sign-in.
  if (tapListener) return;
  tapListener = await PushNotifications.addListener(
    "pushNotificationActionPerformed",
    (action) => {
      const data = (action.notification.data ?? {}) as Record<string, string>;
      const workflowId = data.workflowId;
      if (!workflowId) return;
      // The native shell is a static export: every workflow shares one page
      // and the real id travels as ?id=. See WorkflowRouteFromUrl.
      window.location.assign(
        `/workflows/app?id=${encodeURIComponent(workflowId)}`,
      );
    },
  );
}
