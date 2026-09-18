"use client";
import { useEffect, useState } from "react";

// The current time, re-read once a second while `active`. A running run's
// duration is computed at render, so without its own clock it only moves
// when a poll happens to re-render it, and reads as stepping every few
// seconds instead of counting up.
export function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(tick);
  }, [active]);
  return now;
}
