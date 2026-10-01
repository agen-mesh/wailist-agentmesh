import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

const haptics = vi.hoisted(() => ({ tapFeedback: vi.fn(async () => {}) }));
vi.mock("@/native/haptics", () => haptics);

import { RunProgressDock } from "./RunProgressDock";

const STEPS = [
  { id: "t", name: "Trigger" },
  { id: "a", name: "Triage Agent" },
  { id: "n3", name: "Reply" },
];

function show(props: Partial<React.ComponentProps<typeof RunProgressDock>>) {
  const onDetails = vi.fn();
  const onRunAgain = vi.fn();
  const onDismiss = vi.fn();
  const view = render(
    <RunProgressDock
      steps={STEPS}
      logs={[]}
      runStatus="running"
      onDetails={onDetails}
      onRunAgain={onRunAgain}
      onDismiss={onDismiss}
      {...props}
    />,
  );
  return { ...view, onDetails, onRunAgain, onDismiss };
}

const fill = () =>
  (document.querySelector(".run-dock__fill") as HTMLElement).style.width;

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  try {
    window.localStorage.clear();
  } catch {
    // jsdom always has storage; the component copes without it.
  }
});

describe("the run progress dock", () => {
  it("starts empty, on the first step", () => {
    show({});
    expect(screen.getByText("0/3")).toBeTruthy();
    expect(screen.getByText("Trigger")).toBeTruthy();
    expect(fill()).toBe("0%");
  });

  it("fills as each node answers", () => {
    const { rerender } = show({
      logs: [{ nodeId: "t", status: "success", durationMs: 40 }],
    });
    expect(screen.getByText("1/3")).toBeTruthy();
    expect(fill()).toBe("33%");
    // The next node is the one being worked on.
    expect(screen.getByText("Triage Agent")).toBeTruthy();

    rerender(
      <RunProgressDock
        steps={STEPS}
        logs={[
          { nodeId: "t", status: "success" },
          { nodeId: "a", status: "success" },
        ]}
        runStatus="running"
        onDetails={vi.fn()}
        onRunAgain={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );
    expect(screen.getByText("2/3")).toBeTruthy();
    expect(fill()).toBe("67%");
  });

  it("lists every milestone when expanded, with what each took", () => {
    show({ logs: [{ nodeId: "t", status: "success", durationMs: 1500 }] });
    expect(screen.queryByRole("list")).toBeNull();

    fireEvent.click(screen.getByRole("button", { expanded: false }));
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(3);
    expect(items[0].getAttribute("data-state")).toBe("done");
    expect(items[0].textContent).toContain("1.5s");
    expect(items[1].getAttribute("data-state")).toBe("running");
    expect(items[2].getAttribute("data-state")).toBe("pending");
  });

  it("remembers that it was expanded", () => {
    const first = show({});
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    first.unmount();

    show({});
    expect(screen.getByRole("button", { expanded: true })).toBeTruthy();
  });

  it("names the step that failed, and offers details and another run", () => {
    const { onDetails, onRunAgain } = show({
      logs: [
        { nodeId: "t", status: "success" },
        { nodeId: "a", status: "failed" },
      ],
      runStatus: "failed",
    });

    expect(screen.getByText("Failed at Triage Agent")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(onDetails).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Run again" }));
    expect(onRunAgain).toHaveBeenCalled();
  });

  it("stays put after a failure instead of dismissing itself", () => {
    vi.useFakeTimers();
    try {
      const { onDismiss } = show({
        logs: [{ nodeId: "t", status: "failed" }],
        runStatus: "failed",
      });
      act(() => {
        vi.advanceTimersByTime(30_000);
      });
      expect(onDismiss).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("sees itself out once the run has succeeded", () => {
    vi.useFakeTimers();
    try {
      const { onDismiss } = show({
        logs: STEPS.map((s) => ({ nodeId: s.id, status: "success" })),
        runStatus: "success",
      });
      expect(screen.getByText("Finished")).toBeTruthy();
      expect(fill()).toBe("100%");
      expect(haptics.tapFeedback).toHaveBeenCalledTimes(1);

      expect(onDismiss).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(4000);
      });
      expect(onDismiss).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  // Found on a device: the screen re-renders every poll and passes new
  // inline callbacks, which used to cancel the dismissal timer and leave a
  // finished run docked for good.
  it("still dismisses when the screen re-renders with new callbacks", () => {
    vi.useFakeTimers();
    try {
      const onDismiss = vi.fn();
      const props = {
        steps: STEPS,
        logs: STEPS.map((s) => ({ nodeId: s.id, status: "success" })),
        runStatus: "success",
        onDetails: vi.fn(),
        onRunAgain: vi.fn(),
      };
      const { rerender } = render(
        <RunProgressDock {...props} onDismiss={() => onDismiss()} />,
      );
      // Two more renders, each with a brand-new function, as a poll would.
      act(() => {
        vi.advanceTimersByTime(1000);
      });
      rerender(<RunProgressDock {...props} onDismiss={() => onDismiss()} />);
      act(() => {
        vi.advanceTimersByTime(1000);
      });
      rerender(<RunProgressDock {...props} onDismiss={() => onDismiss()} />);

      act(() => {
        vi.advanceTimersByTime(4000);
      });
      expect(onDismiss).toHaveBeenCalledTimes(1);
      // And the haptic still only fires once, however often it re-renders.
      expect(haptics.tapFeedback).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  // Stop cancels the node in flight, and runner.go records THAT node as
  // failed before finalising the run as stopped. The dock called the
  // reader's own Stop a failure, and useRunDetail stops polling a terminal
  // run, so it stayed that way.
  it("calls a stopped run stopped, not failed", () => {
    const { onDetails, onDismiss } = show({
      logs: [
        { nodeId: "t", status: "success" },
        { nodeId: "a", status: "failed" },
      ],
      runStatus: "stopped",
    });

    expect(screen.getByText("Stopped")).toBeTruthy();
    expect(screen.queryByText(/^Failed at /)).toBeNull();
    expect(
      document.querySelector(".run-dock")!.getAttribute("data-state"),
    ).toBe("stopped");
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(onDetails).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalled();
  });

  // Stop between levels cancels nothing, so there is no log at all. The dock
  // read that as "not started yet" and sat on "Starting…" with no way out.
  it("gives a stop between steps a headline and a way out", () => {
    const { onDismiss } = show({ logs: [], runStatus: "stopped" });

    expect(screen.getByText("Stopped")).toBeTruthy();
    expect(screen.queryByText("Starting…")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalled();
  });

  it("stays put after a stop instead of dismissing itself", () => {
    vi.useFakeTimers();
    try {
      const { onDismiss } = show({ logs: [], runStatus: "stopped" });
      act(() => {
        vi.advanceTimersByTime(30_000);
      });
      expect(onDismiss).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("says so when the run's detail cannot be read", () => {
    const { onDismiss } = show({
      logs: [],
      runStatus: "",
      detailError: "Network error",
    });

    expect(screen.getByText("Cannot read this run")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalled();
  });

  it("will not start a second run while one is starting", () => {
    const { onRunAgain } = show({
      logs: [{ nodeId: "t", status: "failed" }],
      runStatus: "failed",
      busy: true,
    });
    const again = screen.getByRole("button", { name: "Starting…" });
    expect((again as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(again);
    expect(onRunAgain).not.toHaveBeenCalled();
  });

  // It is fixed to the viewport, so no ancestor's padding reaches it: in
  // landscape a cutout or the navigation bar would sit over the buttons.
  it("keeps clear of the cutout on every side", () => {
    show({});
    const style = document.querySelector(".run-dock style")!.textContent!;
    expect(style).toMatch(/\.run-dock \{[\s\S]*?--safe-left/);
    expect(style).toMatch(/\.run-dock \{[\s\S]*?--safe-right/);
    expect(style).toMatch(/\.run-dock \{[\s\S]*?--safe-bottom/);
  });

  // Expanded, a workflow with enough nodes grew taller than a compact
  // landscape screen and pushed its own collapse control off the top.
  it("scrolls its steps rather than outgrowing the screen", () => {
    const many = Array.from({ length: 40 }, (_, i) => ({
      id: `n${i}`,
      name: `Step ${i}`,
    }));
    show({ steps: many, logs: [] });
    fireEvent.click(screen.getByRole("button", { expanded: false }));

    expect(screen.getByText("Step 39")).toBeTruthy();
    // The control that collapses it again is still there to be pressed.
    expect(screen.getByRole("button", { expanded: true })).toBeTruthy();
    const style = document.querySelector(".run-dock style")!.textContent!;
    expect(style).toMatch(/\.run-dock \{[\s\S]*?max-height/);
    expect(style).toMatch(/\.run-dock__steps \{[\s\S]*?overflow-y: auto/);
  });

  // Making the step list scroll cost the other axis: a box with overflow-y
  // set computes overflow-x to auto rather than visible, and the working
  // dot's 1.5x pulse was sliced flat against the list's left edge.
  it("leaves the pulsing dot room inside the scrolling list", () => {
    show({});
    const style = document.querySelector(".run-dock style")!.textContent!;
    expect(style).toMatch(/\.run-dock__steps \{[\s\S]*?padding: 0 4px/);
    expect(style).toMatch(/\.run-dock__steps \{[\s\S]*?margin: 10px -4px 0/);
  });

  it("gives its only expand control a thumb-sized target", () => {
    show({});
    const style = document.querySelector(".run-dock style")!.textContent!;
    expect(style).toMatch(/\.run-dock__line \{[\s\S]*?min-height: 44px/);
    expect(style).toContain(".run-dock__line:focus-visible");
  });

  it("says where the run is for a screen reader", () => {
    show({ logs: [{ nodeId: "t", status: "success" }] });
    const dock = document.querySelector(".run-dock")!;
    expect(dock.getAttribute("aria-live")).toBe("polite");
    expect(dock.getAttribute("aria-label")).toBe(
      "Run progress: 1 of 3 steps. Triage Agent",
    );
  });

  // Motion is the one thing this screen adds, so it has to be switchable off.
  it("turns its motion off under reduced motion", () => {
    show({});
    const style = document.querySelector(".run-dock style")!.textContent!;
    expect(style).toContain("@media (prefers-reduced-motion: reduce)");
    expect(style).toMatch(/prefers-reduced-motion[\s\S]*animation: none/);
    expect(style).toMatch(/prefers-reduced-motion[\s\S]*transition: none/);
  });
});
