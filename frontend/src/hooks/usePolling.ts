"use client";
import { useEffect, useRef } from "react";

// Calls `poll` every `intervalMs` while the screen is visible, and once more
// the moment it becomes visible again. A hidden screen has nobody to show a
// change to, and a phone coming back to the foreground is exactly when
// something may have happened elsewhere, such as a run started on the website.
//
// The latest `poll` is always the one called, so a caller can pass an inline
// function without restarting the timer on every render.
export function usePolling(poll: () => void, intervalMs: number): void {
  const pollRef = useRef(poll);
  useEffect(() => {
    pollRef.current = poll;
  }, [poll]);

  useEffect(() => {
    const visible = () => document.visibilityState === "visible";
    const timer = setInterval(() => {
      if (visible()) pollRef.current();
    }, intervalMs);
    const onVisibility = () => {
      if (visible()) pollRef.current();
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [intervalMs]);
}
