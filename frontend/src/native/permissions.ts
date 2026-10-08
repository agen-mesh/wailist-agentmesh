// Asking for background location, and what to do when the answer is no.
//
// Android's "Always allow" is the most-refused permission on the platform, and
// apps that ask cold get refused far more often than apps that explain first.
// The system dialog is also a one-shot in practice: a user who picks "Don't
// allow" cannot be asked again from inside the app, only sent to Settings. So
// the order here is deliberate -- explain, then ask -- and it is not just
// politeness, it is the difference between the feature working and not.
//
// Google Play reviews background-location use specifically, and asks to see
// the disclosure. The copy below is written to be shown, not to be a comment.
import { Geofence } from "./nativeGeofence";

export type PermissionState = "granted" | "denied" | "denied-permanently";

export const DISCLOSURE = {
  title: "Run workflows when you arrive or leave",
  body:
    "AgentMesh checks your precise location to start a workflow when you " +
    "enter or leave a zone you choose, including when the app is closed " +
    "or not in use.\n\n" +
    "This phone sends each crossing position to our server to check it " +
    "against your zone. The server stores the zone, your latest inside or " +
    "outside state, and the latest reading time. New geofence run history " +
    "records the enter or leave event and time, without the position at " +
    "that crossing.\n\n" +
    "Crossing positions wait in a queue on this phone until they can be " +
    "sent. Accepted readings are removed from the queue. Readings at least " +
    "24 hours old are discarded when the app next processes the queue.\n\n" +
    "You can turn location triggers off at any time. Other app features " +
    "work without background location access.",
  grant: "Choose location access",
  decline: "Not now",
} as const;

// Requests background location. Call ONLY after the disclosure above has been
// shown and the user has chosen to continue -- asking first and explaining
// afterwards is the pattern that gets refused.
export async function requestBackgroundLocation(): Promise<PermissionState> {
  const { granted, reason } = await Geofence.requestPermission();
  if (granted) return "granted";
  // Refused at the foreground step means the user dismissed the first dialog
  // outright; refused at background means they chose "While using the app",
  // which is not enough for a fence that has to hold with the phone pocketed.
  // Both are refusals, but only the second is worth a second conversation.
  return reason === "background" ? "denied" : "denied-permanently";
}

// What the app says once permission is refused.
//
// It does not nag. A refusal is an answer, and re-prompting on every launch is
// how an app gets uninstalled. The geofence feature simply shows as off, with
// one honest route to Settings for anyone who changes their mind.
export const DENIED_COPY = {
  title: "Location triggers are off",
  body:
    "AgentMesh will not start workflows when you arrive or leave. Everything " +
    "else -- viewing, running and chatting with your workflows -- works as " +
    "normal.",
  action: "Open Settings",
} as const;

// Sending someone to Settings is the only route back after a refusal --
// Android will not show the permission dialog again once it has been declined.
export async function openSettings(): Promise<void> {
  await Geofence.openSettings();
}
