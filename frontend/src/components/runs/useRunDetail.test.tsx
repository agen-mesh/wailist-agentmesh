import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api", () => ({ runs: { get } }));

import { useRunDetail } from "./useRunDetail";

const detail = (status: string) => ({
  run: {
    id: "run-1",
    workflowId: "wf-1",
    triggeredBy: "manual",
    status,
    startedAt: "2026-09-21T10:00:00Z",
  },
  logs: [],
  deadLetters: [],
});

afterEach(() => {
  get.mockReset();
  vi.useRealTimers();
});

describe("useRunDetail", () => {
  // One dropped request used to end polling for good, leaving the sheet on
  // "Running" after the run had finished.
  it("keeps polling a running run after a failed request", async () => {
    vi.useFakeTimers();
    get
      .mockResolvedValueOnce(detail("running"))
      .mockRejectedValueOnce(new Error("network"))
      .mockResolvedValueOnce(detail("success"));
    const { result } = renderHook(() => useRunDetail("run-1"));

    await act(async () => {});
    expect(result.current.run?.status).toBe("running");

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.error).toBe("network");

    await act(async () => vi.advanceTimersByTimeAsync(4_000));
    expect(result.current.run?.status).toBe("success");
    expect(result.current.error).toBeNull();
    expect(get).toHaveBeenCalledTimes(3);
  });

  it("stops once the run has finished", async () => {
    vi.useFakeTimers();
    get.mockResolvedValue(detail("success"));
    renderHook(() => useRunDetail("run-1"));

    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(get).toHaveBeenCalledTimes(1);
  });
});
