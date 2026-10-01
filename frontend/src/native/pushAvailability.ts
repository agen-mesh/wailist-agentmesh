// Whether this build can use push notifications at all.
//
// Asked of our own Android plugin (PushAvailabilityPlugin.java) and not of
// @capacitor/push-notifications, because asking that plugin is exactly what
// kills the app: with no google-services.json its register() and unregister()
// throw natively, and the process dies before any promise can reject. Every
// call into it has to be gated on this first.
import { registerPlugin } from "@capacitor/core";

interface PushAvailabilityPlugin {
  check(): Promise<{ available: boolean }>;
}

const PushAvailability =
  registerPlugin<PushAvailabilityPlugin>("PushAvailability");

let answer: Promise<boolean> | null = null;

// Cached for the life of the process: whether Firebase was built in cannot
// change while the app runs. Any failure counts as unavailable, which covers
// the web build and an older native shell without the plugin.
export function pushAvailable(): Promise<boolean> {
  answer ??= PushAvailability.check().then(
    (r) => r.available === true,
    () => false,
  );
  return answer;
}
