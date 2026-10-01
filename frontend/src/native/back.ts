import type { PluginListenerHandle } from "@capacitor/core";

// What the Android Back gesture does.
//
// @capacitor/app claims the back press whether or not anything listens for it,
// and with no listener it only steps back through the WebView's history when
// there is history to step through. On the first screen there is none, so Back
// did nothing at all: the app could not be left with it, and Android's
// predictive back-to-home preview never played.
//
// So Back still walks the WebView history, which is what lets a sheet close on
// Back (hooks/useCloseOnBack adds a history entry for it), and at the start of
// that history it sends the app to the background the way Home does. Minimise
// rather than finish: the app keeps its place, as any other app does on Back.

let backListener: PluginListenerHandle | null = null;

export async function listenForBack(): Promise<void> {
  // Idempotent. A second listener would step back twice per press.
  if (backListener) return;
  const { App } = await import("@capacitor/app");
  backListener = await App.addListener("backButton", ({ canGoBack }) => {
    if (canGoBack) {
      window.history.back();
      return;
    }
    void App.minimizeApp();
  });
}
