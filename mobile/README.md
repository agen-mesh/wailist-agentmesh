# AgentMesh for Android

The native shell: a workflow **viewer and controller**, and the client for the
**geofence trigger**. It is not a mobile editor — the node canvas stays on the
desktop, deliberately (see issue #112).

It wraps the same Next.js frontend the web app ships, built as a static export
and served from the device. What makes it an app rather than a bookmark is the
geofence plugin: the OS watches a boundary and wakes the app on a crossing,
which no web page can do.

## Why Capacitor and not React Native

The canvas is hand-rolled SVG. Capacitor runs it unchanged; React Native would
mean rebuilding all of it against `react-native-svg` for no background-location
benefit, since the same plugin ships for both.

## Layout

```
mobile/
  capacitor.config.ts   app id, https scheme
  scripts/copy-web.mjs  moves the frontend export into www/
  www/                  build output (gitignored)
  android/
    app/src/main/java/ai/agentmesh/app/
      GeofencePlugin.java    Capacitor plugin over Android's GeofencingClient
      GeofenceReceiver.java  receives crossings when the app is not running
```

**Where the TypeScript lives.** The WebView-side code — the API client, session
storage, the offline queue, permissions and the geofence bridge — is in
`frontend/src/native/`, not here. That is not a filing accident: it runs inside
the web page, so it must be compiled by the bundler that builds the page.
Turbopack refuses to compile source outside the Next project root, and four
ways of getting around that (path aliases absolute and relative, a widened
`turbopack.root`, and a `file:` package whose symlink realpath still points
outside) were each tried and each failed.

What is left in this directory is what genuinely cannot live anywhere else:
the native Java plugin, the Gradle project, the Capacitor config and the build
scripts.

## Setup

```bash
cd mobile && npm install && npx cap add android
```

`android/` is generated, and is committed once created so the Gradle config,
manifest and signing setup are reviewable — its build artefacts are ignored.

## Build and run

```bash
npm run sync
```

That builds the frontend as a static export, copies it into `www/`, and runs
`cap sync android`. Then `npm run open:android` for Android Studio, or
`npm run run:android` for an attached device.

The API URL is baked in at build time:

```bash
NEXT_PUBLIC_API_URL=https://api.agentmesh.example npm run sync
```

## How it differs from the web build

Three things the web app relies on do not exist in a WebView, and each has a
real answer rather than a shim:

|                | Web                                                     | Native shell                                                   |
| -------------- | ------------------------------------------------------- | -------------------------------------------------------------- |
| API calls      | proxied through Next's `/api` rewrite, same-site cookie | backend's absolute URL, `Authorization: Bearer`                |
| Session        | HttpOnly `agentmesh_token` cookie                       | token in `@capacitor/preferences` (EncryptedSharedPreferences) |
| Workflow route | `/workflows/<id>`, rendered on demand                   | one prerendered shell, real id as `?id=`                       |

The cookie cannot work here: the WebView origin is `https://localhost`, so the
cookie set on the API's domain is third-party and Android declines to send it,
and `CORS_ORIGIN` is a single origin anyway. The backend has always accepted
`Authorization: Bearer` for non-browser clients; sign-in now returns the token
to a caller that identifies itself with `X-AgentMesh-Client`.

## WebView hardening

Two things, and one of them is a trap.

**WebView debugging is off in release builds, and on in debug builds.** That is
already true without configuring anything: Capacitor defaults
`android.webContentsDebuggingEnabled` to whether the app is debuggable
(`CapConfig` reads `FLAG_DEBUGGABLE`), which is exactly the behaviour wanted.

**Do not set that key in `capacitor.config.ts`.** It looks like the obvious
hardening and it is a regression either way: `false` also removes
`chrome://inspect` from debug builds, for no gain, since release was already
closed; `true` ships a release whose WebView, network traffic and storage are
readable by anyone with a USB cable. The file is one static value baked in at
`cap sync` time and cannot vary by build type, so there is no correct value to
put there. `MainActivity` re-asserts the safe answer natively after
`super.onCreate`, so a future edit to the config cannot ship an inspectable
release by accident.

**A Content-Security-Policy ships in the native bundle only.** It is a `<meta>`
tag rather than a header, because the bundle is files on the device and there
is no server to send one; it is gated on `IS_NATIVE` (which `lib/nativeAuth.ts`
derives from the `NEXT_PUBLIC_NATIVE_CLIENT=1` set by `build:web`) so the web
app, which sits behind Vercel's own headers, never sees it. Built by
`frontend/src/lib/csp.ts`.

`connect-src` names the origin from `NEXT_PUBLIC_API_URL`, so a build with
that unset or malformed would produce an app whose every request is blocked,
reporting it only to a console nobody is reading. `npm run sync` therefore runs
`scripts/check-api-url.mjs` first and refuses to build without a usable one.

The directive that earns its keep is `connect-src`, scoped to the API origin
this build was compiled against -- **both `https://` and `wss://`**, since the
Tendril terminal opens a WebSocket and a policy naming only the https origin
silently blocks it. `font-src` is closed entirely, which is safe because the
fonts are self-hosted, and is worth keeping closed so a future dependency
cannot quietly reintroduce a font-CDN fetch.

What it does not do, stated plainly: `script-src` and `style-src` both carry
`'unsafe-inline'`. Next's static export inlines its hydration payload, and the
nonce that would replace it must be minted per response by a server there is
none of; the app also styles with inline `style` attributes throughout. So this
is not a defence against XSS in the app's own code, and should not be described
as one.

`frame-ancestors` is deliberately absent: a `<meta>`-delivered policy ignores
it by specification, and including it only produces a console error on every
launch.

## Read-only is deliberate here

`frontend/src/lib/device.ts` classifies this WebView as a handheld — through
three independent rungs, so it is guaranteed rather than incidental — and the
app therefore runs in viewer mode. **That is the intended outcome, not an
accident**, and nothing overrides it. Running, stopping and chatting with a
workflow are not withheld from a viewer, which is everything this app needs.

## Geofencing without a paid SDK

This uses **Android's own `GeofencingClient`** through a small plugin in the app
module, rather than a commercial background-tracking SDK.

The distinction that makes that viable: we do not want continuous background
_tracking_, only "did this device cross the edge of one circle". That is
exactly what the platform API does, it is free, and the OS batches the work
across every app on the device — far cheaper on battery than any polling loop.

The alternative (`@transistorsoft/capacitor-background-geolocation`) is a fine
product but **requires a purchased licence for RELEASE builds**, which every
Play Store build is. It was trialled here and removed.

**Where the seam is.** When the app is running, `src/geofence.ts` flushes the
queue. When it is _not_ — the common case for a real crossing —
`GeofenceReceiver.java` appends the fix straight to the same queue, in the same
storage `@capacitor/preferences` uses, and the TypeScript drains it on next
launch without knowing native wrote it.

**The honest limit:** that makes delivery _late_ (next app open) rather than
immediate. Immediate delivery needs an HTTP POST and a WorkManager retry chain
written natively in that receiver. That is the real remaining cost of not
paying, and it is contained rather than unknown — the server already tolerates
late and out-of-order fixes by design, so nothing downstream changes when it
lands.

## Location permission

Background location is the most-refused permission on Android, and asking cold
gets refused far more often than explaining first. `src/permissions.ts` holds
the disclosure shown _before_ the system dialog, and the refusal path: the app
does not nag, the feature simply shows as off, and everything else keeps
working. Google Play reviews background-location use specifically and will ask
to see that disclosure.

## Release

Not on the Railway/Vercel push pipeline. Signed AAB via Gradle, gated on a
release tag or manual dispatch. Validate background behaviour on a real device
through Play's internal-testing track — it skips full review — before any
production submission. Expect at least one resubmit on the background-location
review.

Keystores are never committed; signing material comes from CI secrets.
