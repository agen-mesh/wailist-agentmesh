import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { createRef } from "react";
import type { RunDetailState } from "./useRunDetail";
import type { RunSummary } from "@/lib/types";

// The sheet is tested against a fixed detail rather than the network, and the
// markdown renderer is replaced by plain text so the test only checks what the
// sheet chooses to show.
const state = vi.hoisted(() => ({ detail: null as RunDetailState | null }));

vi.mock("./useRunDetail", () => ({
  useRunDetail: () => state.detail,
}));

vi.mock("@/components/canvas/chat/MarkdownContent", () => ({
  MarkdownContent: ({ text }: { text: string }) => <p>{text}</p>,
}));

import { RunSheet } from "./RunSheet";

const RUN: RunSummary = {
  id: "r-1",
  workflowId: "wf-1",
  workflowName: "Weather Agent",
  triggeredBy: "geofence",
  status: "success",
  startedAt: "2026-09-14T10:00:00.000Z",
  finishedAt: "2026-09-14T10:00:08.000Z",
  spendUsdMicros: 65_000,
};

function detail(overrides: Partial<RunDetailState> = {}): RunDetailState {
  return {
    run: {
      id: "r-1",
      workflowId: "wf-1",
      triggeredBy: "geofence",
      status: "success",
      startedAt: RUN.startedAt,
      finishedAt: RUN.finishedAt,
      spendUsdMicros: RUN.spendUsdMicros,
    },
    logs: [
      {
        id: "l-2",
        runId: "r-1",
        stepIndex: 1,
        nodeId: "n2",
        nodeType: "agent",
        status: "success",
        output: { message: "It is 14°C and partly cloudy." },
        durationMs: 4400,
        ts: "2026-09-14T10:00:08.000Z",
      },
      {
        id: "l-1",
        runId: "r-1",
        stepIndex: 0,
        nodeId: "n1",
        nodeType: "tool402",
        status: "success",
        output: {
          txId: "TX1",
          settledUsdMicros: 65_000,
          nodeName: "x402 Weather",
        },
        durationMs: 1900,
        ts: RUN.startedAt,
      },
    ],
    deadLetters: [],
    error: null,
    loading: false,
    ...overrides,
  };
}

afterEach(async () => {
  cleanup();
  // Defensive: if a fake-timers test above failed before reaching its own
  // vi.useRealTimers(), the setTimeout below would never fire and this
  // afterEach would hang the next test.
  vi.useRealTimers();
  state.detail = null;
  // The sheet removes its history entry with history.back(), which dispatches
  // popstate in a later task. cleanup() has already taken the listeners off;
  // this lets that task run before the next test mounts a sheet of its own.
  await new Promise((resolve) => setTimeout(resolve, 0));
  window.history.replaceState({}, "");
});

describe("RunSheet", () => {
  it("shows the result, the steps in order and what was paid", () => {
    state.detail = detail();
    render(<RunSheet run={RUN} onClose={() => {}} />);

    expect(screen.getByRole("dialog", { name: "Weather Agent" })).toBeTruthy();
    expect(screen.getByText("It is 14°C and partly cloudy.")).toBeTruthy();
    expect(screen.getByText("Location")).toBeTruthy();

    const steps = screen.getByRole("region", { name: "Steps" });
    const names = Array.from(steps.querySelectorAll("li")).map(
      (li) => li.firstChild?.textContent,
    );
    expect(names).toEqual(["x402 Weather", "Agent"]);

    const payments = screen.getByRole("region", { name: "Payments" });
    expect(payments.textContent).toContain("x402 Weather");
    expect(payments.textContent).toContain("$0.065");
  });

  it("lists problems a failed run left behind", () => {
    state.detail = detail({
      deadLetters: [
        {
          id: "d-1",
          runId: "r-1",
          nodeId: "n2",
          error: "provider key rejected",
          attemptCount: 3,
          createdAt: "2026-09-14T10:00:08.000Z",
        },
      ],
    });
    render(<RunSheet run={{ ...RUN, status: "failed" }} onClose={() => {}} />);
    expect(
      screen.getByRole("region", { name: "Problems" }).textContent,
    ).toContain("provider key rejected");
  });

  it("times the run from the detail once it has loaded", () => {
    const base = detail();
    state.detail = {
      ...base,
      run: { ...base.run!, startedAt: "2026-09-14T10:00:05.000Z" },
    };
    render(<RunSheet run={RUN} onClose={() => {}} />);
    // 10:00:05 to 10:00:08 from the detail, not 10:00:00 from the row.
    expect(screen.getByText("3s")).toBeTruthy();
  });

  it("shows spend from the polled detail, not the static row it opened from", () => {
    const base = detail();
    state.detail = {
      ...base,
      run: { ...base.run!, spendUsdMicros: 120_000 },
    };
    // The row prop still carries its original 65_000 — the detail's 120_000
    // must win.
    render(<RunSheet run={RUN} onClose={() => {}} />);
    expect(screen.getByText("$0.120")).toBeTruthy();
  });

  it("falls back to the row's spend before the detail has loaded", () => {
    state.detail = { ...detail(), run: null, loading: true };
    render(<RunSheet run={RUN} onClose={() => {}} />);
    // Scoped to the fact row rather than a bare getByText: with no detail
    // loaded there is no Payments section to collide with, but this stays
    // correct even if that changes.
    const spentLabel = screen.getByText("Spent");
    expect(spentLabel.nextElementSibling?.textContent).toBe("$0.065");
  });

  it("ticks the duration once a second while the run is running", () => {
    vi.useFakeTimers();
    const base = detail();
    state.detail = {
      ...base,
      run: {
        ...base.run!,
        status: "running",
        startedAt: "2026-09-14T10:00:00.000Z",
        finishedAt: undefined,
      },
    };
    vi.setSystemTime(new Date("2026-09-14T10:00:03.000Z"));
    render(<RunSheet run={{ ...RUN, status: "running" }} onClose={() => {}} />);
    expect(screen.getByText("3s")).toBeTruthy();

    // Advancing the fake clock (not re-setting it) moves Date.now() forward
    // AND fires the 1s interval — the tick, not a second render trigger, is
    // what should be updating the duration text.
    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(screen.getByText("7s")).toBeTruthy();
    vi.useRealTimers();
  });

  it("says a run is loading before its detail arrives", () => {
    state.detail = { ...detail(), run: null, logs: [], loading: true };
    render(<RunSheet run={RUN} onClose={() => {}} />);
    expect(screen.getByText("Loading this run…")).toBeTruthy();
  });

  it("closes on Escape and returns focus to the row that opened it", () => {
    state.detail = detail();
    const onClose = vi.fn();
    const opener = createRef<HTMLButtonElement>();
    render(
      <>
        <button ref={opener}>Open run</button>
        <RunSheet run={RUN} onClose={onClose} returnFocusTo={opener} />
      </>,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(opener.current);
  });
});
