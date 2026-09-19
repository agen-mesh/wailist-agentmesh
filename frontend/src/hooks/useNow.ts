"use client";
import { useEffect, useState } from "react";

// The current time, re-read every `intervalMs` (a second by default) while
// `active`. A running run's duration is computed at render, so without its
// own clock it only moves when a poll happens to re-render it, and reads as
// stepping every few seconds instead of counting up. Labels that only change
// by the minute, like "in 4 min", can tick far less often.
export function useNow(active: boolean, intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const tick = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(tick);
  }, [active, intervalMs]);
  return now;
}
