// Navigation that starts outside React, inside the Android app.
//
// A notification tap and the OAuth callback both arrive from the native side,
// where there is no router. They used to call window.location.assign(), and in
// the app that is a full page load. Capacitor's local server answers any path
// without a file extension with the root index.html, so the load opened the
// launch page, replayed the splash, and the launch redirect sent the user to
// /workflows or /signin with the workflow id or the error reason dropped.
//
// So those paths hand the destination to the mounted router instead. NativeBoot
// registers the router here. While the launch page is still deciding where to
// go, the destination is held, and the launch page takes it in place of its
// default.
//
// Deliberately free of React and Capacitor imports, so native code can call it
// and it can be tested on its own.

export interface InAppRoute {
  href: string;
  // Replace the current history entry instead of adding one. For sign-in
  // results, so Back does not return to the sign-in screen.
  replace: boolean;
}

// Returns false when the router cannot take the route yet.
export type InAppNavigator = (route: InAppRoute) => boolean;

let navigator: InAppNavigator | null = null;
let pending: InAppRoute | null = null;

export function setInAppNavigator(next: InAppNavigator | null): void {
  navigator = next;
}

export function navigateInApp(
  href: string,
  options: { replace?: boolean } = {},
): void {
  const route: InAppRoute = { href, replace: options.replace ?? false };
  if (navigator?.(route)) {
    pending = null;
    return;
  }
  pending = route;
}

// The held destination, once. A second call returns null, so a route is never
// followed twice.
export function takePendingRoute(): InAppRoute | null {
  const route = pending;
  pending = null;
  return route;
}

// Where a route may actually go, given whether there is a session.
//
// The shell has no middleware (the static export never runs Next's server), so
// nothing else stops a notification tapped after sign-out from opening a
// workflow screen whose fetch then fails and bounces to /workflows, losing the
// target. A protected route goes through sign-in instead, and sign-in's `next`
// brings the user back to it.
export function gateRoute(route: InAppRoute, signedIn: boolean): InAppRoute {
  if (signedIn || route.href.startsWith("/signin")) return route;
  return {
    href: `/signin?next=${encodeURIComponent(route.href)}`,
    replace: true,
  };
}
