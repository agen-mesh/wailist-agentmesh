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
  /**
   * The finished reply, once `done`. Present because the build's own POST
   * may never reach the browser: the backend runs the build on a context
   * detached from the request, so a proxy timeout ends the response while
   * the build carries on and saves. See waitForFinishedBuild.
   */
  reply?: string;
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
): { stop: (timeoutMs?: number) => Promise<void> } {
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
    // Bounded: the build has already finished by the time stop() is called,
    // and the canvas update and the chat turn both wait on it. A progress
    // request stalled in the proxy must not hold them hostage.
    stop: async (timeoutMs = 3000) => {
      if (stopped) return;
      clearInterval(timer);
      const flush = (async () => {
        await inFlight;
        try {
          const p = await fetchProgress();
          if (!stopped) onProgress(p);
        } catch {
          /* the build response still completes the turn */
        }
      })();
      let timeout: ReturnType<typeof setTimeout> | undefined;
      await Promise.race([
        flush,
        new Promise<void>((resolve) => {
          timeout = setTimeout(resolve, timeoutMs);
        }),
      ]);
      clearTimeout(timeout);
      stopped = true;
    },
  };
}

/**
 * How a build whose request failed actually ended, as far as the progress
 * endpoint can tell.
 *
 * - finished: it completed and saved; `reply` is what the chat shows.
 * - ended: it is over without an answer (it failed, or the workflow changed
 *   under it), and nothing was saved.
 * - unknown: no answer within the wait, or the server has no record of it
 *   (a backend restart forgets builds in flight).
 */
export type BuildOutcome =
  | { kind: "finished"; reply: string }
  | { kind: "ended" }
  | { kind: "unknown" };

export interface WaitOptions {
  /** Receives every snapshot, so the steps keep appearing while it waits. */
  onProgress?: (p: BuildProgress) => void;
  intervalMs?: number;
  /** Longest wait; the backend's own build budget is 240s. */
  maxWaitMs?: number;
  /** Polls in a row with no record of the build before giving up on it. */
  maxEmptyPolls?: number;
  /** Failed polls in a row before giving up. */
  maxFailedPolls?: number;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
}

/**
 * Waits for a build whose request failed to actually end, and says how.
 *
 * The backend detaches a build from its request on purpose, so a closed tab
 * or a proxy timeout does not throw away work already under way. The
 * consequence is that a failed request says nothing about the build: past
 * the proxy window it runs on, saves, and records its reply. Asking once, at
 * the moment the request died, nearly always caught it still running, and
 * the chat reported "build failed" for a build that finished seconds later.
 *
 * Only the progress endpoint's own `done` settles it. The endpoint answers
 * "not done, no steps" for a build it has never heard of, so a run of such
 * answers ends the wait early instead of spinning for minutes. A real build
 * has steps within its first round.
 */
export async function waitForFinishedBuild(
  fetchProgress: () => Promise<BuildProgress>,
  {
    onProgress,
    intervalMs = 2000,
    maxWaitMs = 300_000,
    maxEmptyPolls = 3,
    maxFailedPolls = 5,
    sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    now = () => Date.now(),
  }: WaitOptions = {},
): Promise<BuildOutcome> {
  const deadline = now() + maxWaitMs;
  let empty = 0;
  let failed = 0;
  for (;;) {
    try {
      const p = await fetchProgress();
      failed = 0;
      if (p.done) {
        return p.reply && p.reply.trim() !== ""
          ? { kind: "finished", reply: p.reply }
          : { kind: "ended" };
      }
      onProgress?.(p);
      if (p.steps.length === 0 && !p.current) {
        empty += 1;
        if (empty >= maxEmptyPolls) return { kind: "unknown" };
      } else {
        empty = 0;
      }
    } catch {
      failed += 1;
      if (failed >= maxFailedPolls) return { kind: "unknown" };
    }
    if (now() + intervalMs > deadline) return { kind: "unknown" };
    await sleep(intervalMs);
  }
}
