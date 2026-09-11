// Live progress for a chat build: what the builder is doing, step by step,
// instead of a bare "working…" spinner.
//
// The backend records each step (nodes.BuildProgress) and the chat polls
// GET /workflows/{id}/build/progress about once a second while the build's
// POST is outstanding. Polling rather than a stream because the builder's
// traffic goes through the /api rewrite proxy, which does not reliably hold a
// long-lived event stream open -- see useRunTranscript.ts for the incident.
//
// Pure and framework-free (the poller only takes a fetch function), so the
// merge rules and the final-flush behaviour are unit-tested directly.

import type { ChatMessage } from "./useChatSession";

export interface BuildStep {
  /** search | check | node | edge | x402 | look */
  kind: string;
  label: string;
  status: "done" | "error";
  detail?: string;
}

export interface BuildProgress {
  steps: BuildStep[];
  /** What is in flight right now, if anything. */
  current?: string;
  done?: boolean;
}

/** Steps kept on a stored chat turn; the most recent are the ones kept. */
export const MAX_STORED_STEPS = 40;

/**
 * A build id the backend accepts (^[A-Za-z0-9_-]{8,64}$). Not
 * crypto.randomUUID: it is unavailable on http:// origins in some browsers,
 * and this only needs to be unique per user, not unguessable -- the backend
 * scopes progress to the user and workflow anyway.
 */
export function newBuildId(): string {
  const part = () => Math.random().toString(36).slice(2, 10);
  return `b-${Date.now().toString(36)}-${part()}${part()}`;
}

/**
 * Put a progress snapshot on the pending turn with this id. A settled turn is
 * never touched, and a snapshot with fewer finished steps than the turn
 * already shows is ignored: polls can land out of order, and a newer
 * snapshot never has fewer steps than an older one.
 */
export function setProgressIn(
  messages: ChatMessage[],
  id: string,
  progress: BuildProgress,
): ChatMessage[] {
  const idx = messages.findIndex((m) => m.id === id && m.pending);
  if (idx < 0) return messages;
  const steps = progress.steps.slice(-MAX_STORED_STEPS);
  const have = messages[idx].steps?.length ?? 0;
  if (steps.length < have) return messages;
  const next = [...messages];
  next[idx] = { ...next[idx], steps, current: progress.current || undefined };
  return next;
}

/** "9 steps · 1 issue" */
export function stepsSummary(steps: BuildStep[]): string {
  const n = steps.length;
  const issues = steps.filter((s) => s.status === "error").length;
  const base = `${n} step${n === 1 ? "" : "s"}`;
  return issues > 0 ? `${base} · ${issues} issue${issues === 1 ? "" : "s"}` : base;
}

/**
 * Polls fetchProgress every intervalMs, handing each snapshot to onProgress,
 * until stop() -- which polls one final time, so steps finished between the
 * last poll and the build's response still appear. A failed poll is skipped,
 * not fatal: progress is a nicety, and the build's own response is what
 * completes the turn.
 */
export function startProgressPolling(
  fetchProgress: () => Promise<BuildProgress>,
  onProgress: (p: BuildProgress) => void,
  intervalMs = 1000,
): { stop: () => Promise<void> } {
  let stopped = false;
  let inFlight: Promise<void> = Promise.resolve();

  const poll = () => {
    inFlight = fetchProgress()
      .then((p) => {
        if (!stopped) onProgress(p);
      })
      .catch(() => {
        /* skip this tick */
      });
    return inFlight;
  };

  const timer = setInterval(() => {
    if (!stopped) void poll();
  }, intervalMs);

  return {
    stop: async () => {
      if (stopped) return;
      clearInterval(timer);
      await inFlight;
      try {
        const p = await fetchProgress();
        onProgress(p);
      } catch {
        /* the build response still completes the turn */
      }
      stopped = true;
    },
  };
}
