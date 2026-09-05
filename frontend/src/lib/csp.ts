// The Content-Security-Policy the native shell ships with.
//
// Applied ONLY inside the Android app (see app/layout.tsx). The web app is
// served by Vercel behind its own headers and has a different set of origins to
// think about; giving both the same policy would mean it fits neither.
//
// Built here as a pure function of the API URL rather than written out as a
// literal, because the one directive that actually matters -- connect-src --
// has to name the backend, and the backend differs between a debug build
// pointed at a laptop and a release build pointed at production. A hardcoded
// policy would be wrong in one of those cases, and wrong in a way that only
// shows up at runtime.

/**
 * The origin of the API, or null when there is none.
 *
 * Empty is a real, supported configuration: an unset NEXT_PUBLIC_API_URL puts
 * lib/api.ts into mock mode, and a build in that state must still get a valid
 * policy rather than one containing the word "null".
 */
export function apiOrigin(apiUrl: string | undefined): string | null {
  const raw = (apiUrl ?? "").trim();
  if (raw === "") return null;
  try {
    return new URL(raw).origin;
  } catch {
    // A malformed URL is the build's problem, not the policy's: returning null
    // yields a policy permitting only 'self', which is at least never a policy
    // permitting something unintended.
    //
    // It is NOT loud, and an earlier version of this comment claimed it was.
    // A connect-src that has collapsed to 'self' blocks every request to the
    // backend and says so only in a console nobody is watching on a phone --
    // precisely the silent failure this file warns about for the missing wss://
    // case. Being right about the risk elsewhere in the file and wrong here is
    // how a comment stops being worth reading.
    //
    // The volume is supplied at build time instead: mobile/scripts/
    // check-api-url.mjs refuses to build a native bundle without a usable
    // NEXT_PUBLIC_API_URL, so this branch should be unreachable in any app that
    // ships. It stays defensive because the web build has no such guard and
    // does not need one -- an unset URL is a supported mock mode there.
    return null;
  }
}

/**
 * The same origin with a WebSocket scheme.
 *
 * Not decoration: the Tendril terminal opens `new WebSocket(...)` against the
 * API host (components/canvas/TerminalTab.tsx does
 * `WS_BASE.replace(/^http/, "ws")`), and connect-src matches the full origin
 * INCLUDING its scheme. A policy listing only https://api.example.com blocks
 * wss://api.example.com -- the terminal would simply never connect, with the
 * reason visible only in a console nobody is watching on a phone.
 */
export function websocketOrigin(origin: string): string {
  return origin.replace(/^http/, "ws");
}

/**
 * Builds the policy for a native build talking to apiUrl.
 *
 * What this genuinely buys, stated honestly rather than generously:
 *
 *   - connect-src is the real prize. It is the difference between a compromised
 *     dependency being able to post a session token anywhere it likes, and
 *     being able to reach only our own backend.
 *   - object-src, base-uri and form-action close old, cheap injection routes
 *     that cost nothing to shut.
 *
 * What it does NOT buy, and must not be described as buying:
 *
 *   - script-src carries 'unsafe-inline'. Next's static export inlines its
 *     hydration payload as `self.__next_f.push(...)`, and the nonce that would
 *     replace it must be minted per response by a server. There is no server
 *     here -- the bundle is files on the device. So this is not a defence
 *     against XSS in the app's own code, and saying otherwise would be the same
 *     overstatement native/auth.ts was corrected for.
 *   - style-src likewise: this codebase styles with inline style attributes
 *     throughout (Card, the buttons module, every screen), which is exactly
 *     what 'unsafe-inline' governs.
 */
export function buildCsp(apiUrl: string | undefined): string {
  const origin = apiOrigin(apiUrl);
  const connect = ["'self'"];
  if (origin) {
    connect.push(origin, websocketOrigin(origin));
  }

  const directives: Array<[string, string[]]> = [
    ["default-src", ["'self'"]],
    // See the doc comment: neither of these can be tightened in a static export
    // without a server to mint nonces.
    ["script-src", ["'self'", "'unsafe-inline'"]],
    ["style-src", ["'self'", "'unsafe-inline'"]],
    // Connector logos and user avatars come from arbitrary https hosts, and a
    // broken image is a cosmetic failure rather than a security one. data: and
    // blob: carry generated marks and canvas exports.
    ["img-src", ["'self'", "data:", "blob:", "https:"]],
    // Fonts are self-hosted on purpose (layout.tsx), so this can be closed
    // completely -- and should be, so a future dependency cannot quietly
    // reintroduce a fetch to a font CDN.
    ["font-src", ["'self'"]],
    ["connect-src", connect],
    ["media-src", ["'self'", "data:", "blob:"]],
    ["worker-src", ["'self'", "blob:"]],
    ["object-src", ["'none'"]],
    ["base-uri", ["'self'"]],
    ["form-action", ["'self'"]],
    // frame-ancestors is deliberately absent. It is one of the directives the
    // spec says a <meta>-delivered policy MUST ignore (along with sandbox and
    // report-uri), and Chrome says so out loud: "The Content Security Policy
    // directive 'frame-ancestors' is ignored when delivered via a <meta>
    // element." Since there is no server here to send a header, including it
    // would buy nothing and cost a console error on every single launch --
    // which is how real errors end up being scrolled past.
    //
    // It would also be protecting against nothing: this policy applies inside
    // the Android WebView, and there is no outer page that could frame it.
  ];

  return directives
    .map(([name, values]) => `${name} ${values.join(" ")}`)
    .join("; ");
}
