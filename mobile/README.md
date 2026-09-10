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

## Push notifications

Run-status notifications go out through Firebase Cloud Messaging. Everything is
wired and inert: with no Firebase project configured the app builds and runs
normally, it simply never receives a notification, and the server's send path
does nothing at all.

Turning it on needs two artefacts, and they are not the same kind of thing.

**`google-services.json` — config, not a secret.** In the Firebase console,
create a project and add an Android app whose package name is exactly
`ai.agentmesh.app`; it must match `applicationId` in `app/build.gradle`
character for character, and a mismatch fails silently at delivery time rather
than at build time. Download the file to:

```
mobile/android/app/google-services.json
```

It is **gitignored**. Not because it is confidential -- it ships inside every
APK, so anyone with the app already has a copy -- but because it is per-project
config, and committing one would pin every developer and every CI run to a
single Firebase project. `app/build.gradle` applies the Google Services plugin
only when the file is present, which is why its absence costs nothing.

**A service-account key — a real secret.** Firebase console → Project settings
→ Service accounts → Generate new private key. Give the resulting JSON to the
backend as `FCM_SERVICE_ACCOUNT_JSON`. Never commit it.

Note that the backend uses **FCM HTTP v1**, which authenticates with that
service account. Older guides describe copying a "Server key" from Cloud
Messaging settings; Google has retired that API, and anything still mentioning
a server key is out of date.

**Which runs notify:** ones the user did not start -- geofence, schedule,
webhook -- plus every failure, whatever started it. A run you pressed Run on
does not notify on success, because you are already looking at the screen that
shows the result. The rule lives in one tested function,
`push.ShouldNotify` in `backend/internal/push`.

**Testing needs a real device or an emulator image with Google Play services.**
A plain AVD image has no FCM and will never receive anything.

**Where a user turns them on.** Account menu (the avatar, top right) ->
Notifications. Native builds only: there is no FCM in a browser, so the item is
not rendered there.

The sheet shows one of four states, and they are not interchangeable:

| State       | What it means                                                                                                                                                                             |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| off         | Not on. Either Android has never been asked, or it was asked, said yes, and notifications were switched off here anyway. Shows `PUSH_DISCLOSURE`, then asks Android if it still needs to. |
| granted     | Permission granted **and** turned on here. Offers a way back off.                                                                                                                         |
| denied      | Refused. Android will not ask again, so the only route back is Settings, and the sheet says so instead of offering a retry that would do nothing.                                         |
| unavailable | No Firebase in this build, or no Play services. Nobody refused anything, so no route to Settings is offered.                                                                              |

The state is read with `notificationState()`, which combines
`checkPermissions()` with the stored opt-in, and never requests. Both halves
matter: switching notifications off in the app cannot revoke Android's
permission -- nothing in an app can -- so permission alone kept reading
"granted" the instant after the user turned them off, and the sheet snapped
back to its "on" panel. Not requesting matters too: on Android 13+ the permission
dialog is a one-shot, and using `enablePush()` to find out what to draw would
spend the single ask on rendering a switch.

Turning it on is remembered on the device (`agentmesh.push.optIn`, plain
preferences -- it is a preference, not a credential) and re-registered on the
next launch that restores a session. Two reasons: FCM rotates tokens, and
`push.ts` keeps the current one in memory only, so without the re-arm there
would be nothing to unregister and the switch could not be moved back. Signing
out clears the flag, so the next person to use the phone is not registered for
notifications they were never asked about.

## Location permission

Background location is the most-refused permission on Android, and asking cold
gets refused far more often than explaining first. `src/permissions.ts` holds
the disclosure shown _before_ the system dialog, and the refusal path: the app
does not nag, the feature simply shows as off, and everything else keeps
working. Google Play reviews background-location use specifically and will ask
to see that disclosure.

## Session token storage, and its known dead end

The session token lives in `EncryptedSharedPreferences`, behind
`SecureStorePlugin` in the app module. AES-GCM ciphertext on disk, with the key
in the Android Keystore. What that does _not_ buy is any defence against code
running as this app on an unlocked device: anything the app can decrypt, an
attacker holding the app's identity can decrypt too.

**`androidx.security:security-crypto` is deprecated by Google with no
replacement.** This is known, not an oversight. The pin is `1.0.0` rather than
the `1.1.0` alpha because the stable line is the lesser of the two problems,
and `MasterKeys` on 1.0.0 takes its arguments in a different order from
`MasterKey.Builder`, which only exists from the alpha onwards.

The practical failure mode is key invalidation: the Keystore entry can become
undecryptable (a restored backup, a changed lock screen on some OEM builds),
and the library throws rather than degrading. `SecureStorePlugin` handles that
by deleting the corrupt file and starting a fresh store, which costs the
session but not the app. `native/auth.ts` handles the same failure from the
other side by keeping the user signed in on whatever the old store still holds.

Whoever replaces this will need to pick a successor, and there is no official
one. Tink directly is the usual answer.

## Release

Not on the Railway/Vercel push pipeline. Signed AAB via Gradle, gated on a
release tag or manual dispatch. Validate background behaviour on a real device
through Play's internal-testing track — it skips full review — before any
production submission. Expect at least one resubmit on the background-location
review.

Keystores are never committed; signing material comes from CI secrets.

### The version comes from the tag

`versionCode` and `versionName` are derived in `android.yml` from the release
tag: `android-v1.2.3` gives `versionName 1.2.3` and `versionCode 10203`, as
`major*10000 + minor*100 + patch`.

Computed from the version rather than from `github.run_number`, so it is a pure
function of the tag: re-running a build, or rebuilding after an infrastructure
failure, produces the identical `versionCode` instead of burning a number Play
would then refuse to reuse. The scheme caps minor and patch at 99, so the
workflow **fails** on a tag that exceeds it rather than silently emitting a
colliding code, and on a malformed tag rather than guessing.

A `workflow_dispatch` run has no tag and falls through to the `build.gradle`
defaults, `1` / `0.0.0-dev`. A local build is not a release and should not be
able to look like one in a bug report.

### Release prerequisites

Everything below needs a human, and most of it needs a human with Play Console
access. None of it can be done from inside this repository.

> **Back up the keystore, outside this repository.** It exists in one place:
> the `ANDROID_KEYSTORE_BASE64` secret, which GitHub will not show you again.
> If it is lost the app can never be updated under the same Play listing. Not
> "with difficulty": the listing is frozen, and you would publish a new app
> under a new package name and ask every user to reinstall. Put the `.jks` and
> its three passwords in a password manager and somewhere offline.

**Upload the `.aab`, not the APK.** Only the APK is attached to the GitHub
Release; the bundle is in the workflow run's artifacts (`agentmesh-release-aab`,
**7-day retention**). If it has expired, re-run the workflow or cut a new tag.
Publishing is deliberately manual: a release should be a decision somebody
makes, not a side effect of pushing a tag.

**Privacy policy URL.** Mandatory once background location is declared, and
Play checks that it resolves: `https://www.agent-mesh.app/privacy`. The page is
`frontend/src/app/privacy/page.tsx`, drafted from what the app actually does.
It is a binding document and Play will hold you to it, so have someone
qualified read it before the listing goes live.

**Data Safety form.** Must match behaviour or the submission is rejected, and
being caught overstating is worse than a slow review. Grounded in the code:
location is collected (approximate and precise), **not** shared with third
parties, and **not stored on our servers**. Migration `000029` keeps only the
derived `geofence_inside` boolean and a timestamp, never coordinates. It is
processed ephemerally, is not required to use the app, and clearing the zone
removes the stored state. One nuance to declare rather than hide: undelivered
fixes are held **on the device** while offline and deleted once sent or within
a day (`frontend/src/native/queue.ts`).

**Background-location declaration.** A dedicated Google review, separate from
the normal one. They want a written justification and **a short video** showing
the in-app disclosure appearing _before_ the system dialog, and the app already
does this (`frontend/src/native/permissions.ts`), and it is the single biggest
factor in these reviews. Budget calendar time, not engineering time: one to
three weeks, and rejections are more often about the video than the app.

**Store listing.** Phone screenshots at minimum, short and full descriptions,
icon, feature graphic, content rating questionnaire.

What still cannot be verified here: that the `.aab` installs from the
internal-testing track on a real device. That needs hardware, and belongs to
the deferred device-verification issue.
