import type { PluginListenerHandle } from "@capacitor/core";
import { auth } from "@/lib/api";
import { SecureStore } from "./secureStore";

// OAuth sign-in from inside the app.
//
// The buttons on the sign-in screen used to do `window.location.href = <api
// origin>/auth/oauth/google`, which navigates the WebView off its own
// https://localhost origin. The native CSP's `default-src 'self'` blocks that;
// when it does not, the app bundle is gone and there is no way back but killing
// the app. Either way no session arrives, because the backend finishes by
// setting a cookie the WebView will not accept.
//
// So the provider is opened OUTSIDE the WebView and the answer comes back as an
// Android intent:
//
//   start()  -> Custom Tab at <frontend>/api/auth/oauth/<provider>?client=android&challenge=
//               ... provider, consent, backend callback ...
//   intent   -> ai.agentmesh.app://auth?code=<one-time>
//   handle() -> POST /auth/oauth/exchange {code, verifier} -> session token
//
// Outside the WebView is not a preference. Google refuses OAuth in an embedded
// WebView outright (`disallowed_useragent`), so a Custom Tab or the system
// browser is the only thing that works -- and a Custom Tab is the better of the
// two, because it shares Chrome's cookie jar and the user is usually signed in
// there already.

// The scheme is the app's own applicationId. It must match, exactly, three
// other places: capacitor.config.ts's appId, custom_url_scheme in
// mobile/android/app/src/main/res/values/strings.xml, and nativeAppScheme in
// backend/internal/api/handlers/oauth_native.go. A mismatch fails silently --
// Android simply does not match the intent-filter, the Custom Tab sits open,
// and nothing anywhere says why.
const APP_SCHEME = "ai.agentmesh.app";

// Persist through process death while the external browser is open.
const VERIFIER_KEY = "agentmesh.oauth.verifier";
// Where sign-in was headed, kept beside the verifier for the same reason: the
// app can be killed while the Custom Tab is in front, and the callback then
// arrives in a fresh process that has to know where to go.
const NEXT_KEY = "agentmesh.oauth.next";

// 32 bytes, hex. Long enough that guessing it is not a strategy, and printable
// so it survives storage with no encoding questions.
function newVerifier(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// PKCE S256: base64url, unpadded. The same transform the backend applies to the
// verifier when it checks, so the two must not drift.
async function challengeOf(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return btoa(String.fromCharCode(...new Uint8Array(digest)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

// start opens the provider's consent screen in a Custom Tab.
//
// Throws when the app has no backend configured, because there is nothing
// useful to open and a Custom Tab showing an error page is worse than a
// sentence on the sign-in screen.
export async function start(
  provider: "github" | "google",
  next?: string,
): Promise<void> {
  const base = await auth.nativeOAuthURL(provider);
  if (!base) throw new Error("Social sign in is not configured.");

  const verifier = newVerifier();
  await SecureStore.set({ key: VERIFIER_KEY, value: verifier });
  if (next) await SecureStore.set({ key: NEXT_KEY, value: next });
  else await SecureStore.remove({ key: NEXT_KEY });
  lastCallbackURL = null;

  const url = `${base}?client=android&challenge=${encodeURIComponent(
    await challengeOf(verifier),
  )}`;

  const { Browser } = await import("@capacitor/browser");
  try {
    await Browser.open({ url });
  } catch (error) {
    await SecureStore.remove({ key: VERIFIER_KEY });
    await SecureStore.remove({ key: NEXT_KEY });
    throw error;
  }
}

// The outcome of one callback, so the caller decides what to show. A thrown
// error would be indistinguishable from a bug in the listener, and this runs
// where nobody is awaiting a promise.
// `next` is where sign-in was headed when it started, if anywhere; the caller
// decides whether it is safe to follow.
export type OAuthResult =
  | { ok: true; token: string; next?: string }
  | { ok: false; reason: string; next?: string };

// handleCallbackUrl turns a deep link into a session, or into a reason.
//
// Exported, and takes the URL rather than reading an event, so every decision
// in it can be tested without a Capacitor bridge.
export async function handleCallbackUrl(url: string): Promise<OAuthResult> {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return { ok: false, reason: "bad_callback" };
  }

  if (!isCallbackURL(parsed)) return { ok: false, reason: "bad_callback" };

  let next: string | undefined;
  try {
    const { value: verifier } = await SecureStore.get({ key: VERIFIER_KEY });
    await SecureStore.remove({ key: VERIFIER_KEY });
    next = (await SecureStore.get({ key: NEXT_KEY })).value ?? undefined;
    await SecureStore.remove({ key: NEXT_KEY });
    const error = parsed.searchParams.get("error");
    if (error) return { ok: false, reason: error, next };
    const code = parsed.searchParams.get("code");
    if (!code) return { ok: false, reason: "no_code", next };
    if (!verifier) return { ok: false, reason: "no_verifier", next };

    const token = await auth.oauthExchange(code, verifier);
    return token
      ? { ok: true, token, next }
      : { ok: false, reason: "exchange_failed", next };
  } catch {
    return { ok: false, reason: "exchange_failed", next };
  }
}

function isCallbackURL(url: URL): boolean {
  return (
    url.protocol === `${APP_SCHEME}:` &&
    url.hostname === "auth" &&
    (url.pathname === "" || url.pathname === "/")
  );
}

let urlListener: PluginListenerHandle | null = null;
let listening: Promise<void> | null = null;
let lastCallbackURL: string | null = null;

// Attach before reading the initial intent, then deduplicate either delivery.
export function listenForCallback(
  onResult: (result: OAuthResult) => void | Promise<void>,
): Promise<void> {
  if (listening) return listening;
  listening = (async () => {
    const { App } = await import("@capacitor/app");
    const deliver = async (url: string) => {
      try {
        if (!isCallbackURL(new URL(url))) return;
      } catch {
        return;
      }
      if (lastCallbackURL === url) return;
      lastCallbackURL = url;
      // Android retains the initial intent across WebView navigations. Once
      // consumed, a page reload must not exchange it again or undo sign-in.
      const pending = await SecureStore.get({ key: VERIFIER_KEY });
      if (!pending.value) return;
      const result = await handleCallbackUrl(url);
      try {
        const { Browser } = await import("@capacitor/browser");
        await Browser.close();
      } catch {
        // Android may have already closed the tab.
      }
      await onResult(result);
    };
    urlListener = await App.addListener("appUrlOpen", (event) => {
      void deliver(event.url).catch(() => {
        console.error("Could not deliver the sign-in result.");
      });
    });
    const launch = await App.getLaunchUrl();
    if (launch?.url) await deliver(launch.url);
  })().catch(async (error) => {
    await urlListener?.remove();
    urlListener = null;
    listening = null;
    throw error;
  });
  return listening;
}
