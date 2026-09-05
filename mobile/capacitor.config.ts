import type { CapacitorConfig } from "@capacitor/cli";

// The native Android shell.
//
// It wraps the same Next.js frontend the web app ships, built as a static
// export (frontend/next.config.ts, MOBILE_BUILD=1) and copied into www/. What
// makes it an app rather than a bookmark is the geofence plugin in
// android/app/src/main/java/ai/agentmesh/app: the OS watches the boundary and
// wakes us on a crossing, which no web page can do.
const config: CapacitorConfig = {
  appId: "ai.agentmesh.app",
  appName: "AgentMesh",
  webDir: "www",

  // android.webContentsDebuggingEnabled is deliberately NOT set here, and it
  // should stay that way.
  //
  // It looks like the obvious hardening -- set it false and a release build
  // cannot be inspected. It is not. Capacitor already defaults the value to
  // whether the app is debuggable (CapConfig reads FLAG_DEBUGGABLE), so the
  // behaviour we want is what happens with the key absent: debug builds are
  // inspectable, release builds are not.
  //
  // Setting it FALSE would apply to both, taking chrome://inspect away from
  // developers for no security gain -- release was already closed. Setting it
  // TRUE ships a release build whose WebView, network traffic and storage are
  // readable by anyone with a USB cable. There is no value here that is better
  // than leaving it out, because this file is one static value baked into the
  // bundle at `cap sync` time and cannot vary by build type.
  //
  // MainActivity re-asserts the safe answer natively, so a future edit to this
  // file cannot ship an inspectable release by accident.

  server: {
    // https, not the default capacitor:// -- the WebView treats an https
    // origin as a secure context, which the geolocation and notification APIs
    // both require. It also keeps the origin stable across releases, which
    // matters because localStorage and IndexedDB are keyed on it.
    androidScheme: "https",
  },
};

export default config;
