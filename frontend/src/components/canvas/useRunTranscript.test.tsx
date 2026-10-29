import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useRunTranscript } from "./useRunTranscript";

const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api", () => ({
  runs: { get },
  isSettledLogStatus: (status: string) => ["success", "failed", "stopped", "degraded"].includes(status),
}));

const waitingLog = { stepIndex: 1, nodeId: "purchase", nodeType: "purchase", status: "waiting", output: { id: "order-one" }, durationMs: 1, ts: "2026-10-30T00:00:00Z" };
beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  get.mockReset().mockResolvedValue({ run: { status: "waiting" }, logs: [waitingLog], deadLetters: [] });
});
afterEach(() => { cleanup(); vi.useRealTimers(); localStorage.clear(); });

it("keeps a durable waiting purchase incomplete after the polling budget", async () => {
  const complete = vi.fn();
  const { result } = renderHook(() => useRunTranscript({ runId: "run-one", running: true, onRunComplete: complete, workflowId: "workflow-one" }));
  await act(async () => { await vi.advanceTimersByTimeAsync(30 * 60 * 1000 + 100); });
  expect(result.current.done).toBe(false);
  expect(complete).not.toHaveBeenCalled();
  expect(result.current.logs.some((row) => row.status === "waiting")).toBe(true);
  expect(JSON.parse(localStorage.getItem("agentmesh_lastrun_workflow-one")!).waiting).toBe(true);
});

it("reconnects a cached waiting run and completes only after a terminal record", async () => {
  localStorage.setItem("agentmesh_lastrun_workflow-one", JSON.stringify({ runId: "run-one", waiting: true, logs: [waitingLog], elapsed: 3 }));
  const complete = vi.fn();
  const { result } = renderHook(() => useRunTranscript({ runId: null, running: false, onRunComplete: complete, workflowId: "workflow-one" }));
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(get).toHaveBeenCalledWith("run-one");
  expect(result.current.done).toBe(false);
  expect(result.current.stopped).toBe(false);
  get.mockResolvedValue({ run: { status: "success" }, logs: [{ ...waitingLog, status: "success" }], deadLetters: [] });
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  expect(result.current.done).toBe(true);
  expect(complete).toHaveBeenCalledOnce();
});
