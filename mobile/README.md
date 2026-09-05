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
