"use client";
import { useEffect, useState } from "react";
import {
  runs,
  type DeadLetterRun,
  type RunDetail,
  type RunLogRecord,
} from "@/lib/api";

export interface RunDetailState {
  run: RunDetail | null;
  logs: RunLogRecord[];
  deadLetters: DeadLetterRun[];
  error: string | null;
  loading: boolean;
}

// Polling interval while the run is still going. Short enough that a step
// finishing shows up promptly, long enough not to hammer the backend.
const POLL_MS = 2_000;
// A failed poll is retried, backing off from POLL_MS to this.
const MAX_RETRY_MS = 30_000;

const EMPTY: Omit<RunDetailState, "loading"> = {
  run: null,
  logs: [],
  deadLetters: [],
  error: null,
};

// One run's detail for the run sheet: fetched once, re-fetched every two
// seconds only while the run is running, and stopped when the sheet closes
// (runId becomes null or the component unmounts).
//
// Deliberately not useRunTranscript. That hook opens a live stream and writes
// the canvas's "last run" cache when a run finishes, which would replace the
// canvas's own last run with whichever old run was tapped here.
export function useRunDetail(runId: string | null): RunDetailState {
  // Keyed by the run it belongs to, so a new runId shows as loading without
  // resetting state synchronously inside the effect.
  const [state, setState] = useState<{ forId: string | null } & RunDetailState>(
    { forId: null, ...EMPTY, loading: false },
  );

  useEffect(() => {
    if (!runId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    // Whether the run was still going at the last answer (null before one).
    let lastStatus: string | null = null;
    let failures = 0;

    const load = async () => {
      try {
        const data = await runs.get(runId);
        if (cancelled) return;
        failures = 0;
        lastStatus = data.run.status;
        setState({
          forId: runId,
          run: data.run,
          logs: data.logs ?? [],
          deadLetters: data.deadLetters ?? [],
          error: null,
          loading: false,
        });
        if (data.run.status === "running") timer = setTimeout(load, POLL_MS);
      } catch (e) {
        if (cancelled) return;
        setState((prev) => ({
          ...(prev.forId === runId ? prev : { ...EMPTY }),
          forId: runId,
          error: e instanceof Error ? e.message : "Could not load this run.",
          loading: false,
        }));
        // One dropped request must not freeze a running run on "Running"
        // for as long as the sheet is open. A run known to be finished has
        // nothing left to fetch.
        if (lastStatus === null || lastStatus === "running") {
          failures += 1;
          timer = setTimeout(
            load,
            Math.min(POLL_MS * 2 ** failures, MAX_RETRY_MS),
          );
        }
      }
    };
    void load();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [runId]);

  if (!runId) return { ...EMPTY, loading: false };
  if (state.forId !== runId) return { ...EMPTY, loading: true };
  return {
    run: state.run,
    logs: state.logs,
    deadLetters: state.deadLetters,
    error: state.error,
    loading: state.loading,
  };
}
