// A single tap of physical feedback.
//
// Quiet on purpose. Haptics are confirmation, not decoration: fired on every
// press they stop meaning anything and start feeling like a phone with a fault.
// The rule this file follows is that a buzz answers a question the screen
// cannot answer yet -- "has it caught?" during a pull, before any pixel has
// moved to say so.
//
// Native only, and silent everywhere else. A browser has no equivalent worth
// reaching for: navigator.vibrate is a blunt motor pulse rather than the taptic
// engine's short click, iOS ignores it entirely, and Chrome requires a user
// gesture and logs a console error when that is not met. A web user gets
// nothing rather than something worse.
import { IS_NATIVE } from "@/lib/nativeAuth";

/**
 * A short click, for the moment an interaction commits.
 *
 * Never throws and never rejects. A device with no vibrator, a user who has
 * turned haptics off system-wide, and a build with no plugin all reach the same
 * catch, and none of them is a reason for the caller to handle an error: the
 * feedback is an enhancement, and its absence changes nothing about what the
 * app just did.
 */
export async function tapFeedback(): Promise<void> {
  if (!IS_NATIVE) return;
  try {
    // Imported lazily so the plugin never enters the web bundle. IS_NATIVE is a
    // build-time constant, so this branch is eliminated in a browser build and
    // the import is never even requested.
    const { Haptics, ImpactStyle } = await import("@capacitor/haptics");
    await Haptics.impact({ style: ImpactStyle.Light });
  } catch {
    // Deliberately swallowed. See above.
  }
}
