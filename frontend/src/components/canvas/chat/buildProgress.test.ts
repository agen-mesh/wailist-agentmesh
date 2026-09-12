import { describe, it, expect, vi, afterEach } from "vitest";
import {
  newBuildId,
  setProgressIn,
  startProgressPolling,
  stepsSummary,
  MAX_STORED_STEPS,
  type BuildProgress,
  type BuildStep,
} from "./buildProgress";
import type { ChatMessage } from "./useChatSession";

const step = (label: string, status: BuildStep["status"] = "done"): BuildStep => ({
  kind: "node",
  label,
  status,
});

const pending = (id: string): ChatMessage => ({
  id,
  sender: "assistant",
  text: "",
  ts: "2026-09-11T00:00:00Z",
  pending: true,
});

describe("newBuildId", () => {
  it("matches the backend's accepted format and is unique", () => {
    const a = newBuildId();
    const b = newBuildId();
    expect(a).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(a).not.toBe(b);
  });
});

describe("setProgressIn", () => {
  it("puts the steps and current activity on that pending turn only", () => {
    const msgs = [pending("a1"), pending("a2")];
    const next = setProgressIn(msgs, "a2", {
      steps: [step("Added Manual Trigger")],
      current: "Searching the web for “nifty”",
    });
    expect(next[0].steps).toBeUndefined();
    expect(next[1].steps?.map((s) => s.label)).toEqual(["Added Manual Trigger"]);
    expect(next[1].current).toBe("Searching the web for “nifty”");
  });

  it("never touches a turn that has already settled", () => {
    const settled = { ...pending("a1"), pending: false, text: "Done." };
    expect(setProgressIn([settled], "a1", { steps: [step("x")] })).toEqual([settled]);
  });

  // Polls can land out of order. A later snapshot never has fewer finished
  // steps, so an older one must not roll the list back.
  it("ignores a stale snapshot with fewer steps than already shown", () => {
    const first = setProgressIn([pending("a1")], "a1", {
      steps: [step("one"), step("two")],
    });
    const stale = setProgressIn(first, "a1", { steps: [step("one")], current: "old" });
    expect(stale[0].steps?.length).toBe(2);
  });

  it("caps what it keeps, holding on to the most recent steps", () => {
    const many = Array.from({ length: MAX_STORED_STEPS + 10 }, (_, i) => step(`s${i}`));
    const next = setProgressIn([pending("a1")], "a1", { steps: many });
    expect(next[0].steps?.length).toBe(MAX_STORED_STEPS);
    expect(next[0].steps?.at(-1)?.label).toBe(`s${MAX_STORED_STEPS + 9}`);
  });
});

describe("stepsSummary", () => {
  it("counts steps and calls out problems", () => {
    expect(stepsSummary([step("a")])).toBe("1 step");
    expect(stepsSummary([step("a"), step("b", "error"), step("c")])).toBe("3 steps · 1 issue");
  });
});

describe("startProgressPolling", () => {
  afterEach(() => vi.useRealTimers());

  it("polls on an interval and flushes one last time when stopped", async () => {
    vi.useFakeTimers();
    let calls = 0;
    const seen: BuildProgress[] = [];
    const poller = startProgressPolling(
      async () => {
        calls++;
        return { steps: Array.from({ length: calls }, (_, i) => step(`s${i}`)) };
      },
      (p) => seen.push(p),
      1000,
    );
    await vi.advanceTimersByTimeAsync(3000);
    const beforeStop = calls;
    expect(beforeStop).toBeGreaterThanOrEqual(3);
    await poller.stop();
    // The final flush catches the steps finished between the last poll and
    // the build's response -- otherwise the last few steps never appear.
    expect(calls).toBe(beforeStop + 1);
    expect(seen.at(-1)?.steps.length).toBe(calls);
    await vi.advanceTimersByTimeAsync(5000);
    expect(calls).toBe(beforeStop + 1);
  });

  // Review finding: stop() awaited the in-flight poll and a final one with no
  // timeout, so a stalled progress request left the canvas un-updated and
  // the chat composer locked even though the build had already finished.
  it("stops within its timeout even when a poll never answers", async () => {
    vi.useFakeTimers();
    const poller = startProgressPolling(
      () => new Promise<BuildProgress>(() => {}),
      () => {},
      1000,
    );
    await vi.advanceTimersByTimeAsync(1500);
    let stopped = false;
    const stopping = poller.stop(2000).then(() => {
      stopped = true;
    });
    await vi.advanceTimersByTimeAsync(2500);
    await stopping;
    expect(stopped).toBe(true);
  });

  it("keeps polling through a failed request", async () => {
    vi.useFakeTimers();
    let calls = 0;
    const poller = startProgressPolling(
      async () => {
        calls++;
        if (calls === 1) throw new Error("network blip");
        return { steps: [] };
      },
      () => {},
      1000,
    );
    await vi.advanceTimersByTimeAsync(2500);
    expect(calls).toBeGreaterThanOrEqual(2);
    await poller.stop();
  });
});
