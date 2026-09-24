"use client";
import { useEffect, useRef } from "react";

// Calls `poll` every `intervalMs` while the screen is visible, and once more
// the moment it becomes visible again. A hidden screen has nobody to show a
// change to, and a phone coming back to the foreground is exactly when
// something may have happened elsewhere, such as a run started on the website.
//
// The latest `poll` is always the one called, so a caller can pass an inline
// function without restarting the timer on every render.
//
// One poll at a time. A `poll` that returns a promise is not called again
// until that promise settles, however many ticks or foreground events arrive
// meanwhile. Two overlapping requests could otherwise resolve out of order,
// and the older answer, landing last, would overwrite the newer one -- a run
// shown finished would flip back to running.
export function usePolling(
  poll: () => void | Promise<unknown>,
  intervalMs: number,
): void {
  const pollRef = useRef(poll);
  const inFlight = useRef(false);
  useEffect(() => {
    pollRef.current = poll;
  }, [poll]);

  useEffect(() => {
    const tick = () => {
      if (document.visibilityState !== "visible" || inFlight.current) return;
      const result = pollRef.current();
      if (!result) return;
      inFlight.current = true;
      void result
        .catch(() => {})
        .then(() => {
          inFlight.current = false;
        });
    };
    const timer = setInterval(tick, intervalMs);
    const onVisibility = tick;
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [intervalMs]);
}
