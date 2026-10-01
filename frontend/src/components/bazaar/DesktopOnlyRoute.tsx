"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useIsHandheld } from "@/hooks/useIsHandheld";

// The Bazaar is for building workflows, which happens on a computer, so it is
// not offered on a phone -- not in the tab bar and not in the top bar's sheet.
// A link to it can still arrive from a message or a browser history entry, and
// landing on a screen the app otherwise hides is worse than being sent back to
// somewhere useful.
//
// The redirect is here, in a client component, rather than in middleware: a
// handheld is decided by viewport and pointer, which the server cannot see.
// useIsHandheld answers "desktop" until hydration, so children render as
// normal there and only a real handheld ever redirects.
export function DesktopOnlyRoute({
  children,
  to = "/workflows",
}: {
  children: React.ReactNode;
  to?: string;
}) {
  const handheld = useIsHandheld();
  const router = useRouter();

  useEffect(() => {
    // replace, not push: Back from the destination should leave the app, not
    // bounce through a route that immediately redirects again.
    if (handheld) router.replace(to);
  }, [handheld, router, to]);

  // Nothing, rather than a frame of a screen that is about to disappear.
  if (handheld) return null;
  return <>{children}</>;
}
