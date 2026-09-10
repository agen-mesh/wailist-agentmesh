// The native shell's entry point.
//
// Called once from the web bundle when it is running inside Capacitor. Its job
// is to reconnect two halves that are otherwise unaware of each other: the
// session the app persisted, and whatever the OS queued while the app was
// closed.
import { loadToken, saveToken, clearToken } from "./auth";
import { flush, start, stop } from "./geofence";
import { setGeofence, clearGeofence } from "./api";
import { clearOptedIn } from "./pushPrefs";
import {
  disablePush,
  enablePush,
  restorePush,
  listenForTaps,
  notificationState,
  type PushReadState,
  type PushState,
} from "./push";
import { listenForCallback } from "./oauth";

export interface NativeShell {
  onSignedIn(token: string): Promise<void>;
  onSignedOut(): Promise<void>;
  setGeofence(
    workflowId: string,
    fence: { lat: number; lng: number; radiusM: number },
  ): Promise<boolean>;
  clearGeofence(workflowId: string): Promise<void>;
  /**
   * Asks for notification permission and registers this device.
   *
   * Separate from onSignedIn rather than folded into it, because the two want
   * different timing: a session must be restored before the first request goes
   * out, whereas a permission prompt should appear when the user has been
   * given a reason for it. The web bundle decides when that is; the shell only
   * knows how.
   */
  enableNotifications(): Promise<PushState>;
  /**
   * What Android says about notification permission, without asking.
   *
   * A screen that offers a notifications switch has to draw it before the
   * user touches anything, and enableNotifications() cannot answer that
   * question without spending the one-shot permission dialog to do it.
   */
  notificationState(): Promise<PushReadState>;
  /**
   * Turns notifications off and drops this device's registration.
   *
   * The mirror of enableNotifications, and not merely part of signing out.
   * A switch that can only be moved one way is not a switch.
   */
  disableNotifications(): Promise<void>;
}

/**
 * Boots the shell. Safe to call more than once.
 *
 * Returns the token it restored, so the caller can hydrate the web bundle's
 * API client with it -- the shell deliberately does not reach into that module
 * itself, because the direction of the dependency matters: the web bundle
 * knows nothing about Capacitor, and keeping it that way is what lets the same
 * code run in a browser.
 */
export async function boot(): Promise<string | null> {
  const token = await loadToken();
  // Drain anything GeofenceReceiver appended while there was no WebView alive.
  // Not awaited: a flush that cannot reach the network keeps its queue, and
  // the app must still start.
  void flush();
  // Attached unconditionally, before anything is known about permission. A
  // notification tapped from a cold start delivers its event during launch,
  // and a listener registered after that has already missed it -- the app
  // would open on its front page having been asked to open a specific run.
  void listenForTaps().catch(() => {});
  // Re-register with FCM if this device was already turned on for
  // notifications. Two reasons, and the second is the one that is easy to
  // miss:
  //
  //   - FCM rotates tokens. A device that registered a month ago may be
  //     holding a token the server can no longer deliver to, and re-running
  //     the registration is what refreshes it.
  //   - push.ts keeps currentToken in memory only, so after a cold start
  //     nothing knows which row to drop. Without this, a user who turned
  //     notifications on yesterday could not turn them off today.
  //
  // Gated on a restored session, not a signed-out launch: registerDevice is
  // authenticated and would simply 401. And on boot() rather than onSignedIn,
  // because onSignedIn is a FRESH sign-in -- possibly a different person on
  // the same phone -- who has not agreed to anything. onSignedOut clears the
  // flag, so this can only ever re-arm for the person who set it.
  //
  // Not awaited, for the same reason flush() is not: a slow FCM registration
  // must not hold up the launch.
  if (token !== null) {
    void restorePush().catch((err) =>
      console.error("push: could not re-arm on launch", err),
    );
  }
  // Same reasoning, and the same cold start: the OAuth callback arrives as an
  // Android intent, and Android is free to have killed the app while the Custom
  // Tab was in front. A listener attached when the sign-in screen mounts would
  // miss the answer to the question that screen asked.
  //
  // The token goes in through the same seam password sign-in uses rather than
  // through saveToken directly -- see persistNativeSession -- so a failed write
  // rolls the session back instead of leaving the app half signed in.
  void listenForCallback(async (result) => {
    if (!result.ok) {
      window.location.assign(
        `/signin?error=${encodeURIComponent(result.reason)}`,
      );
      return;
    }
    const { persistNativeSession } = await import("@/hooks/useAuth");
    try {
      await persistNativeSession(result.token);
      window.location.assign("/workflows");
    } catch {
      window.location.assign("/signin?error=session_persist");
    }
  }).catch(() => {});
  return token;
}

export const shell: NativeShell = {
  async onSignedIn(token: string) {
    await saveToken(token);
    // A queue that could not flush while signed out is now deliverable.
    void flush();
  },

  async onSignedOut() {
    // Notifications first, and only then the token: unregistering is an
    // authenticated call, so clearing the session first would guarantee it
    // fails and leave this device receiving the next user's run results.
    await disablePush();
    // Belt and braces. disablePush() clears the opt-in already, but it is one
    // `await` away from a plugin call that can throw on a device with no push
    // provider at all, and the cost of the flag surviving a sign-out is that
    // the NEXT person to sign in on this phone is registered for
    // notifications they were never asked about.
    await clearOptedIn();
    await clearToken();
  },

  async enableNotifications() {
    return enablePush();
  },

  async notificationState() {
    return notificationState();
  },

  async disableNotifications() {
    await disablePush();
  },

  async setGeofence(workflowId, fence) {
    // Server first. If the backend rejects the zone -- undeployed workflow,
    // radius out of range -- registering it with the OS would leave the device
    // watching a boundary the server will never act on.
    await setGeofence(workflowId, fence);
    return start({ workflowId, ...fence });
  },

  async clearGeofence(workflowId) {
    // Server first, same reasoning as setGeofence above but in the removal
    // direction: if this throws, the device keeps watching a boundary the
    // server still has armed, which is consistent (if now stale to the user's
    // intent) rather than the alternative -- disarming the OS watch first and
    // then failing to tell the server, which leaves the server believing a
    // fence is live that will never fire again.
    await clearGeofence(workflowId);
    await stop(workflowId);
  },
};
