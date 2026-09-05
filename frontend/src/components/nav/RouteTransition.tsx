"use client";
import { useEffect, useRef } from "react";
import { usePathname } from "next/navigation";
import { useIsHandheld } from "@/hooks/useIsHandheld";

// Gives route changes a direction on a phone.
//
// An instant swap is the loudest remaining tell that this is a website: on a
// phone, screens arrive from somewhere. Going deeper slides in from the right,
// coming back slides in from the left, which is the convention every mobile
// platform uses to say where you are in a stack without a word of copy.
//
// Handheld only. On a desktop a route change is a page load and nobody expects
// it to move.

// Why this is a CSS animation on the incoming screen rather than the View
// Transitions API:
//
// A real push animates BOTH frames -- the outgoing screen leaves as the
// incoming one arrives -- and that needs the browser to hold a snapshot of the
// old DOM across the update, which is exactly what startViewTransition() does.
// Driving it from the App Router means intercepting every navigation, and the
// supported way to do that in Next 16 is still behind an experimental flag.
// This app also builds as a static export for the shell, where that path is
// least tested.
//
// So this animates the arriving screen only. It reads as a screen entering with
// a direction rather than a true stack push, it costs one class and no
// framework risk, and it is a straight swap for startViewTransition() the day
// Next's support settles. Said here rather than leaving the next person to
// wonder why the obvious API was not used.
const FORWARD = "route-enter-fwd";
const BACK = "route-enter-back";

/**
 * Which way a move between two routes reads.
 *
 * Depth, not history: the App Router exposes no navigation type here, and
 * reading `window.navigation` would work in Chrome and quietly do nothing
 * elsewhere. Segment count answers the question this actually asks --
 * `/workflows` to `/workflows/abc` is going deeper, and the reverse is coming
 * back.
 *
 * A sideways move between two routes of the same depth reads as forward, which
 * is the right default for a set of peers: tab bars do not have a "back".
 *
 * Exported so the rule can be tested without a DOM, a router, or a phone.
 */
export function enterDirection(from: string, to: string): "forward" | "back" {
  const depth = (p: string) => p.split("/").filter(Boolean).length;
  return depth(to) < depth(from) ? "back" : "forward";
}

export function RouteTransition({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const handheld = useIsHandheld();
  const ref = useRef<HTMLDivElement>(null);
  // The route this component last animated to. Not state: changing it must not
  // itself cause a render, and the effect below is the only reader.
  const previous = useRef<string | null>(null);

  useEffect(() => {
    const el = ref.current;
    const from = previous.current;
    previous.current = pathname;

    // Nothing to animate INTO on the first paint -- the screen is not arriving
    // from anywhere, it is simply the first one.
    if (!el || !handheld || from === null || from === pathname) return;

    const direction =
      enterDirection(from, pathname) === "back" ? BACK : FORWARD;

    // Remove, force a reflow, re-add: without the reflow the browser coalesces
    // the two class mutations and the animation never restarts, so a second
    // navigation in the same direction would play nothing at all.
    el.classList.remove(FORWARD, BACK);
    void el.offsetWidth;
    el.classList.add(direction);
  }, [pathname, handheld]);

  // A plain wrapper with no styles of its own, so it cannot disturb the layout
  // of anything inside it. Deliberately NOT keyed on the pathname: keying would
  // remount the whole page subtree on every navigation, throwing away component
  // state and re-running every fetch to buy an animation.
  return (
    <div
      ref={ref}
      onAnimationEnd={(e) => {
        // Only this element's own animation. A card animating inside the page
        // bubbles its animationend up here too, and stripping the class on
        // someone else's event would cut the screen transition short.
        if (e.target === e.currentTarget) {
          e.currentTarget.classList.remove(FORWARD, BACK);
        }
      }}
    >
      {children}
    </div>
  );
}
