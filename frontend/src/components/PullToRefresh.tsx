"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { useIsHandheld } from "@/hooks/useIsHandheld";
import { tapFeedback } from "@/native/haptics";

// Pull down at the top of a list to reload it.
//
// Built rather than configured, for a reason worth writing down: the browser's
// own pull-to-refresh is disabled by the `overscroll-behavior-y: contain` that
// makes the app stop rubber-banding like a document. That trade is right --
// rubber-banding is one of the loudest "this is a web page" tells -- but it
// takes the gesture with it, and on a list of workflows whose statuses change
// server-side, pulling to refresh is the first thing a thumb tries.

/** How far the indicator must travel before the pull counts. */
export const PULL_THRESHOLD_PX = 64;

/** Where the indicator parks while the refresh runs. */
export const PULL_RESTING_PX = 48;

/**
 * Finger travel converted to how far the indicator actually moves.
 *
 * Not 1:1. A pull that tracks the finger exactly feels slack, and it lets a
 * long drag haul the indicator halfway down the screen. Resistance that grows
 * with distance is what makes the gesture feel like it is pulling against
 * something: the first pixels move almost freely and it gets progressively
 * heavier, so the threshold is crossed deliberately rather than by accident
 * during an ordinary scroll.
 *
 * Exported so the curve can be tested without a touchscreen.
 */
export function damp(distance: number): number {
  if (distance <= 0) return 0;
  // Square-root falloff, scaled so the threshold sits at a comfortable travel
  // and the tail flattens rather than stopping dead at a cap.
  return Math.round(Math.sqrt(distance) * 6);
}

export function PullToRefresh({
  onRefresh,
  className,
  style,
  children,
}: {
  /** Re-fetch. Resolves when the data has landed; rejection is ignored here. */
  onRefresh: () => Promise<unknown>;
  className?: string;
  style?: React.CSSProperties;
  children: React.ReactNode;
}) {
  const handheld = useIsHandheld();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [offset, setOffset] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const [dragging, setDragging] = useState(false);

  // Gesture state lives in refs, not state: these change on every touchmove
  // frame and none of them should cause a render on their own. Only `offset`
  // does, because only `offset` is drawn.
  const startY = useRef<number | null>(null);
  const armed = useRef(false);
  const refreshingRef = useRef(false);

  const run = useCallback(async () => {
    if (refreshingRef.current) return;
    refreshingRef.current = true;
    setRefreshing(true);
    setOffset(PULL_RESTING_PX);
    try {
      await onRefresh();
    } catch {
      // The list keeps whatever it already had. A failed refresh is the
      // caller's to report -- this component's only job is to stop spinning.
    } finally {
      refreshingRef.current = false;
      setRefreshing(false);
      setOffset(0);
    }
  }, [onRefresh]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !handheld) return;

    const onStart = (e: TouchEvent) => {
      // Only from a genuine rest at the top. Starting mid-scroll would fight
      // the scroll the user is already doing.
      if (refreshingRef.current || el.scrollTop > 0 || e.touches.length !== 1) {
        startY.current = null;
        return;
      }
      startY.current = e.touches[0].clientY;
      armed.current = false;
    };

    const onMove = (e: TouchEvent) => {
      if (startY.current === null) return;
      const dy = e.touches[0].clientY - startY.current;
      if (dy <= 0) {
        // Pulling up is an ordinary scroll; hand it straight back.
        setOffset(0);
        setDragging(false);
        startY.current = null;
        return;
      }
      // Only now, once this is definitely a downward pull from the top, is it
      // right to take the gesture from the browser. Doing it in onStart would
      // swallow every normal downward scroll that begins at the top.
      if (e.cancelable) e.preventDefault();
      setDragging(true);
      const next = damp(dy);
      setOffset(next);
      if (!armed.current && next >= PULL_THRESHOLD_PX) {
        armed.current = true;
        // The buzz lands the moment it catches, which is before anything on
        // screen can say so. This is the whole reason haptics are here.
        void tapFeedback();
      }
    };

    const onEnd = () => {
      if (startY.current === null) return;
      startY.current = null;
      setDragging(false);
      if (armed.current) {
        armed.current = false;
        void run();
      } else {
        setOffset(0);
      }
    };

    // Non-passive, because onMove calls preventDefault. React's synthetic
    // touchmove is passive, so preventDefault there is ignored with a console
    // warning -- the same reason CanvasGraph attaches its wheel listener by
    // hand.
    el.addEventListener("touchstart", onStart, { passive: true });
    el.addEventListener("touchmove", onMove, { passive: false });
    el.addEventListener("touchend", onEnd, { passive: true });
    el.addEventListener("touchcancel", onEnd, { passive: true });
    return () => {
      el.removeEventListener("touchstart", onStart);
      el.removeEventListener("touchmove", onMove);
      el.removeEventListener("touchend", onEnd);
      el.removeEventListener("touchcancel", onEnd);
    };
  }, [handheld, run]);

  return (
    <div className={className} style={{ ...style, position: "relative" }}>
      {/* Above the content and outside it, so a list scrolling under the
          indicator does not push it around. aria-hidden because the state it
          reports is already reported by the list itself refreshing. */}
      {handheld && offset > 0 && (
        <div
          aria-hidden="true"
          className="ptr-indicator"
          data-spinning={refreshing ? "" : undefined}
          style={{ transform: `translate(-50%, ${offset - 28}px)` }}
        >
          <Arrow armed={offset >= PULL_THRESHOLD_PX} spinning={refreshing} />
        </div>
      )}
      <div
        ref={scrollRef}
        style={{
          height: "100%",
          overflow: "auto",
          // Follows the finger while pulling, springs back when let go. No
          // transition during the drag, or the surface would lag the thumb.
          transform: offset > 0 ? `translateY(${offset}px)` : undefined,
          transition: dragging ? undefined : "transform 0.2s var(--ease)",
        }}
      >
        {children}
      </div>
    </div>
  );
}

function Arrow({ armed, spinning }: { armed: boolean; spinning: boolean }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      style={{
        display: "block",
        // Points down while pulling, flips once it will fire. The rotation is
        // the only thing that says "let go now" before the release.
        transform: spinning ? undefined : `rotate(${armed ? 180 : 0}deg)`,
        transition: "transform 0.15s var(--ease)",
      }}
    >
      <path d="M8 2.5v11M3.5 9 8 13.5 12.5 9" />
    </svg>
  );
}
