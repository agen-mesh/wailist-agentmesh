import { describe, it, expect } from "vitest";
import { waitForFinishedBuild, type BuildProgress } from "./buildProgress";

// A scripted progress endpoint plus a fake clock, so the wait runs instantly.
function script(answers: Array<BuildProgress | Error>) {
  let i = 0;
  let clock = 0;
  const calls = { polls: 0 };
  return {
    calls,
    fetch: async () => {
      calls.polls += 1;
      const a = answers[Math.min(i, answers.length - 1)];
      i += 1;
      if (a instanceof Error) throw a;
      return a;
    },
    opts: {
      intervalMs: 1000,
      sleep: async (ms: number) => {
        clock += ms;
      },
      now: () => clock,
    },
  };
}

const running: BuildProgress = { steps: [{ kind: "node", label: "Added X", status: "done" }], current: "Adding Y" };

describe("waitForFinishedBuild", () => {
  // The case this exists for: asked once, the build was still running, and
  // the chat said "build failed" for a build that finished moments later.
  it("keeps waiting while the build runs, then returns its reply", async () => {
    const s = script([running, running, running, { steps: running.steps, done: true, reply: "Built it." }]);
    const got = await waitForFinishedBuild(s.fetch, s.opts);
    expect(got).toEqual({ kind: "finished", reply: "Built it." });
    expect(s.calls.polls).toBe(4);
  });

  it("hands every snapshot on, so the steps keep appearing", async () => {
    const seen: BuildProgress[] = [];
    const s = script([running, { steps: running.steps, done: true, reply: "ok" }]);
    await waitForFinishedBuild(s.fetch, { ...s.opts, onProgress: (p) => seen.push(p) });
    expect(seen).toEqual([running]);
  });

  it("reports a build that ended without an answer", async () => {
    const s = script([{ steps: running.steps, done: true, reply: "" }]);
    expect(await waitForFinishedBuild(s.fetch, s.opts)).toEqual({ kind: "ended" });
  });

  // The endpoint answers "not done, no steps" for a build it never heard of
  // (a backend restart forgets them). That must not spin for five minutes.
  it("gives up early on a build the server has no record of", async () => {
    const s = script([{ steps: [] }]);
    expect(await waitForFinishedBuild(s.fetch, { ...s.opts, maxEmptyPolls: 3 })).toEqual({ kind: "unknown" });
    expect(s.calls.polls).toBe(3);
  });

  // A build still in its first model call has no steps yet but does have a
  // current step, so it is not mistaken for an unknown one.
  it("does not give up on a build that has only just started", async () => {
    const starting: BuildProgress = { steps: [], current: "Thinking" };
    const s = script([starting, starting, starting, starting, { steps: [], done: true, reply: "done" }]);
    expect(await waitForFinishedBuild(s.fetch, { ...s.opts, maxEmptyPolls: 3 })).toEqual({ kind: "finished", reply: "done" });
  });

  it("stops at the deadline", async () => {
    const s = script([running]);
    const got = await waitForFinishedBuild(s.fetch, { ...s.opts, maxWaitMs: 10_000 });
    expect(got).toEqual({ kind: "unknown" });
    expect(s.calls.polls).toBeLessThanOrEqual(11);
  });

  it("rides out a failed poll", async () => {
    const s = script([new Error("blip"), running, { steps: running.steps, done: true, reply: "ok" }]);
    expect(await waitForFinishedBuild(s.fetch, s.opts)).toEqual({ kind: "finished", reply: "ok" });
  });

  it("gives up when the progress endpoint keeps failing", async () => {
    const s = script([new Error("down")]);
    expect(await waitForFinishedBuild(s.fetch, { ...s.opts, maxFailedPolls: 5 })).toEqual({ kind: "unknown" });
    expect(s.calls.polls).toBe(5);
  });
});
