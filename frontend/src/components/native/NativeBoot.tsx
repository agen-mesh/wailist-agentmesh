"use client";
import { useCallback, useEffect, useRef } from "react";
import { usePathname, useRouter } from "next/navigation";
import {
  IS_NATIVE,
  setAuthToken,
  markAuthReady,
  getAuthToken,
  authReady,
} from "@/lib/nativeAuth";
import { auth, BASE } from "@/lib/api";
import {
  gateRoute,
  setInAppNavigator,
  takePendingRoute,
  type InAppRoute,
} from "@/lib/nativeNav";

// Restores the session the native shell persisted, once, on first mount.
//
// The join between two halves that stay ignorant of each other: the shell owns
// durable storage and the OS, the web bundle owns the API client. The token is
// handed across here rather than either side importing the other.
//
// The import is dynamic and guarded by IS_NATIVE, a build-time constant. In a
// browser build that branch is dead, so the Capacitor chunk is never fetched;
// on device it loads once. A static import would pull @capacitor/* into every
// page of the web app to satisfy a branch that never runs there.
//
// boot() talking to the Capacitor bridge is the one step here with no
// built-in timeout of its own -- a WebView-bridge call that never calls back
// (a known failure mode, not a hypothetical one) would otherwise leave
// authReady unresolved forever and useAuth stuck showing a permanent loading
// spinner instead of ever falling back to signed-out. Racing it against a
// timeout turns "hangs forever" into "fails after 10s", which the existing
// .catch/.finally below already handle correctly.
const BOOT_TIMEOUT_MS = 10_000;
function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("native boot timed out")),
      ms,
    );
    p.then(
      (v) => {
        clearTimeout(timer);
        resolve(v);
      },
      (err) => {
        clearTimeout(timer);
        reject(err);
      },
    );
  });
}

// Renders nothing. Mounted in the root layout because the session has to be
// restored before any page makes its first authenticated call, not when some
// particular screen happens to appear.
//
// It also gives the shell a router (lib/nativeNav.ts). A notification tap or an
// OAuth result arrives from native code with nowhere to navigate, and a page
// load would reopen the app on its launch page.
export function NativeBoot() {
  const router = useRouter();
  const pathname = usePathname();
  const pathnameRef = useRef(pathname);
  const navigation = useRef(0);
  useEffect(() => {
    pathnameRef.current = pathname;
    return () => {
      navigation.current += 1;
    };
  }, [pathname]);

  const go = useCallback(
    async (route: InAppRoute) => {
      const request = ++navigation.current;
      let signedIn = !BASE;
      if (BASE && !route.href.startsWith("/signin")) {
        await authReady;
        if (request !== navigation.current) return;
        const token = getAuthToken();
        if (token !== null) {
          try {
            await withTimeout(auth.me(), BOOT_TIMEOUT_MS);
            signedIn = getAuthToken() === token;
          } catch {
            // An unconfirmed session keeps the destination behind sign-in.
            signedIn = false;
          }
        }
      }
      // A newer tap, route change, or unmount supersedes an in-flight check.
      if (request !== navigation.current) return;
      const { href, replace } = gateRoute(route, signedIn);
      if (replace) router.replace(href);
      else router.push(href);
    },
    [router],
  );

  // Registered before boot() runs below (effects run in order), because boot()
  // attaches the listeners that navigate.
  useEffect(() => {
    if (!IS_NATIVE) return;
    setInAppNavigator((route) => {
      // The launch page is still choosing between /workflows and /signin. It
      // takes the held route in place of its default (app/page.tsx).
      if (pathnameRef.current === "/") return false;
      void go(route);
      return true;
    });
    return () => setInAppNavigator(null);
  }, [go]);

  // A route that arrived while the launch page was already redirecting is
  // still held once the redirect lands. Follow it then.
  useEffect(() => {
    if (!IS_NATIVE || pathname === "/") return;
    const held = takePendingRoute();
    if (!held) return;
    void go(held);
  }, [pathname, go]);

  useEffect(() => {
    if (!IS_NATIVE) return;
    let cancelled = false;
    void withTimeout(
      import("@/native").then(({ boot }) => boot()),
      BOOT_TIMEOUT_MS,
    )
      .then((token) => {
        if (!cancelled && token) setAuthToken(token);
      })
      .catch((err) => {
        // A shell that cannot boot must not take the app down with it. The
        // viewer still works signed out; the alternative is a blank screen
        // with the reason visible only in logcat.
        console.error("native shell failed to boot", err);
      })
      .finally(() => {
        // Unconditional, unlike the setAuthToken call above: authReady is a
        // module-level, one-shot signal shared with every mount of this
        // component, not per-mount state. Gating it on `cancelled` would
        // permanently starve useAuth's auth.me() call if this effect were
        // ever cleaned up before boot() settled -- not reachable today with
        // NativeBoot mounted once in the root layout, but nothing enforces
        // that invariant here, and a signal that can hang forever is cheap
        // to just not build in the first place.
        markAuthReady();
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return null;
}
