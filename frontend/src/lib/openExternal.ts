import { IS_NATIVE } from "@/lib/nativeAuth";

// Where the app sends someone to add credits. The Android app does not take
// payment itself -- checkout in the app needs its own payment-provider project
// -- so top-ups happen on the website, in an in-app browser tab.
export const WEB_BILLING_URL = "https://www.agent-mesh.app/billing";

// Opens a page that belongs outside the app: a block explorer, or the
// website's billing page.
//
// In the Android app a plain target="_blank" link hands the page to the system
// browser, which leaves the app with nothing to bring the user back. An in-app
// browser tab closes back to the screen they were on, and onClose runs when it
// does. On the web this is an ordinary new tab, and onClose is not used.
export async function openExternal(
  url: string,
  options: { onClose?: () => void } = {},
): Promise<void> {
  if (!IS_NATIVE) {
    window.open(url, "_blank", "noopener,noreferrer");
    return;
  }
  const { Browser } = await import("@capacitor/browser");
  const { onClose } = options;
  if (onClose) {
    const handle = await Browser.addListener("browserFinished", () => {
      void handle.remove();
      onClose();
    });
  }
  await Browser.open({ url });
}
