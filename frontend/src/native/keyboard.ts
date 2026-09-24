// Whether the on-screen keyboard is open, as Android reports it.
//
// The bottom bar steps aside while the keyboard is up. On the web that has to
// be inferred from the viewport shrinking, which cannot tell a keyboard that
// closed during a rotation from one that stayed open. The app does not have to
// guess: the keyboard plugin forwards the system's own show and hide events.
import { IS_NATIVE } from "@/lib/nativeAuth";

/**
 * Calls `onChange` each time the keyboard opens or closes, and returns the
 * function that stops listening. Null outside the app, where there is nothing
 * to listen to and the caller keeps its own measure.
 */
export function watchKeyboard(
  onChange: (open: boolean) => void,
): (() => void) | null {
  if (!IS_NATIVE) return null;
  let stopped = false;
  const handles: Array<{ remove: () => Promise<void> }> = [];
  // Imported lazily so the plugin never enters the web bundle.
  void import("@capacitor/keyboard")
    .then(async ({ Keyboard }) => {
      const shown = await Keyboard.addListener("keyboardWillShow", () =>
        onChange(true),
      );
      const hidden = await Keyboard.addListener("keyboardWillHide", () =>
        onChange(false),
      );
      if (stopped) {
        void shown.remove();
        void hidden.remove();
        return;
      }
      handles.push(shown, hidden);
    })
    .catch((err) => {
      // The bar then simply stays: the safe way to be wrong.
      console.error("keyboard events unavailable", err);
    });
  return () => {
    stopped = true;
    for (const handle of handles) void handle.remove();
  };
}
