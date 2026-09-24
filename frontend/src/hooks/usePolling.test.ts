import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { usePolling } from "./usePolling";

let visibility: DocumentVisibilityState = "visible";

function setVisibility(state: DocumentVisibilityState) {
  visibility = state;
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(
    () => visibility,
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("usePolling", () => {
  it("polls at the interval while visible", () => {
    const poll = vi.fn();
    renderHook(() => usePolling(poll, 1_000));
    expect(poll).not.toHaveBeenCalled();
    vi.advanceTimersByTime(3_000);
    expect(poll).toHaveBeenCalledTimes(3);
  });

  it("stays quiet while hidden", () => {
    const poll = vi.fn();
    renderHook(() => usePolling(poll, 1_000));
    setVisibility("hidden");
    vi.advanceTimersByTime(5_000);
    expect(poll).not.toHaveBeenCalled();
  });

  it("polls at once on coming back to the foreground", () => {
    const poll = vi.fn();
    renderHook(() => usePolling(poll, 60_000));
    setVisibility("hidden");
    setVisibility("visible");
    expect(poll).toHaveBeenCalledTimes(1);
  });

  it("follows a change of interval", () => {
    const poll = vi.fn();
    const { rerender } = renderHook(({ ms }) => usePolling(poll, ms), {
      initialProps: { ms: 10_000 },
    });
    rerender({ ms: 1_000 });
    vi.advanceTimersByTime(2_000);
    expect(poll).toHaveBeenCalledTimes(2);
  });

  it("calls the latest callback without restarting the timer", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(({ poll }) => usePolling(poll, 1_000), {
      initialProps: { poll: first },
    });
    vi.advanceTimersByTime(500);
    rerender({ poll: second });
    vi.advanceTimersByTime(500);
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("stops when the screen goes away", () => {
    const poll = vi.fn();
    const { unmount } = renderHook(() => usePolling(poll, 1_000));
    unmount();
    vi.advanceTimersByTime(3_000);
    setVisibility("visible");
    expect(poll).not.toHaveBeenCalled();
  });

  // Overlapping requests can resolve out of order, and the older one would
  // land last.
  it("waits for a pending poll before starting another", async () => {
    let settle!: () => void;
    const poll = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          settle = resolve;
        }),
    );
    renderHook(() => usePolling(poll, 1_000));

    vi.advanceTimersByTime(1_000);
    expect(poll).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(5_000);
    setVisibility("visible");
    expect(poll).toHaveBeenCalledTimes(1);

    settle();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(poll).toHaveBeenCalledTimes(2);
  });

  it("carries on after a poll that fails", async () => {
    const poll = vi.fn(() => Promise.reject(new Error("offline")));
    renderHook(() => usePolling(poll, 1_000));

    await vi.advanceTimersByTimeAsync(3_000);
    expect(poll).toHaveBeenCalledTimes(3);
  });
});
