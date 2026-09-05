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

/**
 * Asks for permission, registers with FCM, and tells the server where to find
 * this device.
 *
 * Call only after PUSH_DISCLOSURE has been shown and the user has chosen to
 * continue. Resolves with what actually happened rather than throwing: being
 * refused is an answer, not an error, and every caller wants to carry on
 * either way.
 */
export async function enablePush(): Promise<PushState> {
  try {
    let perm = await PushNotifications.checkPermissions();
    if (perm.receive === "prompt" || perm.receive === "prompt-with-rationale") {
      perm = await PushNotifications.requestPermissions();
    }
    if (perm.receive !== "granted") return "denied";

    const token = await registerForToken();
    if (token === null) return "unavailable";

    await registerDevice(token);
    currentToken = token;
    return "granted";
  } catch (err) {
    // A missing google-services.json lands here, and so does an FCM outage.
    // Neither is worth taking the app down for: notifications are the one
    // feature whose absence the user can simply be told about.
    console.error("push: could not enable notifications", err);
    return "unavailable";
  }
}

/**
 * Stops this device receiving notifications, and tells the server so.
 *
 * Called on sign-out. Never throws: a device that cannot reach the network
 * must still be able to sign out, and the server is not left holding a dead
 * row either way -- FCM rejects sends to an unregistered token, and the send
 * path drops the row on that verdict.
 */
export async function disablePush(): Promise<void> {
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
  // notification anywhere until the next cold start. registerForToken now
  // removes the two listeners it owns as soon as it settles, so there is
  // nothing of this function's to tidy up.
  await PushNotifications.unregister().catch(() => {});
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
      handles.splice(0).forEach(drop);
      resolve(value);
    };

    // Neither event is guaranteed to arrive. A device with no Play services,
    // or one whose google-services.json was never added, can leave both
    // unfired -- and an unresolved promise here would hang sign-in behind a
    // notification the user never asked for.
    const timer = setTimeout(() => finish(null), REGISTRATION_TIMEOUT_MS);

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
