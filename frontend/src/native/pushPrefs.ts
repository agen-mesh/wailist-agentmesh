// Whether this device has been asked for, and given, notification permission.
//
// Android's permission is not enough on its own to answer "should this device
// be registered?". Permission survives a sign-out, and it is granted to the
// app rather than to a person -- so a phone that user A opted in on would,
// with permission alone as the test, silently register user B's session the
// moment they signed in. This flag is the record of somebody having chosen it,
// and it is cleared at sign-out for exactly that reason.
//
// It is also what makes turning notifications OFF work after a restart.
// push.ts keeps its FCM token in memory only (deliberately -- see the comment
// on `currentToken`), so nothing else survives a cold start to say that this
// device is registered at all.
//
// @capacitor/preferences, not the Keystore. This is a preference, not a
// credential: knowing that a phone wants notifications is worth nothing to
// somebody holding the phone, and putting it in the secure store would
// misrepresent what that store is for. queue.ts stores the location queue the
// same way, under the same `agentmesh.` key prefix.
import { Preferences } from "@capacitor/preferences";

const OPT_IN_KEY = "agentmesh.push.optIn";

/**
 * Whether notifications were turned on, on this device, by whoever is signed
 * in now.
 *
 * A read that fails answers false. The consequence of a false negative is that
 * the user is asked again, which is recoverable; the consequence of a false
 * positive is registering a device nobody asked to register, which is not.
 */
export async function hasOptedIn(): Promise<boolean> {
  try {
    const { value } = await Preferences.get({ key: OPT_IN_KEY });
    return value === "1";
  } catch (err) {
    console.error("push: could not read the notification preference", err);
    return false;
  }
}

/** Records that notifications were turned on. Called after a successful enable. */
export async function setOptedIn(): Promise<void> {
  await Preferences.set({ key: OPT_IN_KEY, value: "1" });
}

/**
 * Forgets the opt-in. Called when notifications are turned off, and on
 * sign-out -- the next person to use this phone has not agreed to anything.
 *
 * Never throws. This runs on the sign-out path, which has to complete on a
 * device that cannot write to disk any more than it can reach the network.
 */
export async function clearOptedIn(): Promise<void> {
  await Preferences.remove({ key: OPT_IN_KEY }).catch((err) => {
    console.error("push: could not clear the notification preference", err);
  });
}
