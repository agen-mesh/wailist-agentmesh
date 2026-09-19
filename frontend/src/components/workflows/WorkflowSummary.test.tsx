import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { RunPage, RunSummary, Workflow } from "@/lib/types";

// The summary is tested against a stubbed API. The top bar, the pull gesture
// and the sheet have their own tests, so they are reduced to what this screen
// hands them.
const api = vi.hoisted(() => {
  class RunsUnavailableError extends Error {}
  return {
    RunsUnavailableError,
    get: vi.fn(),
    run: vi.fn(),
    stop: vi.fn(),
    listForWorkflow: vi.fn(),
    runGet: vi.fn(),
  };
});

vi.mock("@/lib/api", () => ({
  RunsUnavailableError: api.RunsUnavailableError,
  workflows: { get: api.get, run: api.run, stop: api.stop },
  runs: { listForWorkflow: api.listForWorkflow, get: api.runGet },
}));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/components/PullToRefresh", () => ({
  PullToRefresh: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/components/runs/RunSheet", () => ({
  RunSheet: ({ run }: { run: RunSummary }) => (
    <div role="dialog">sheet for {run.id}</div>
  ),
}));

import { WorkflowSummary } from "./WorkflowSummary";

function workflow(overrides: Partial<Workflow> = {}): Workflow {
  return {
    id: "wf-1",
    name: "Morning digest",
    status: "deployed",
    nodes: [
      { id: "t", type: "trigger", template: "manual", x: 0, y: 0 },
      { id: "p", type: "provider", x: 0, y: 0 },
    ],
    edges: [],
    ...overrides,
  };
}

const FINISHED: RunSummary = {
  id: "r-1",
  workflowId: "wf-1",
  workflowName: "Morning digest",
  triggeredBy: "schedule",
  status: "success",
  startedAt: new Date(Date.now() - 60_000).toISOString(),
  finishedAt: new Date(Date.now() - 30_000).toISOString(),
  spendUsdMicros: 21_000,
};

function page(runs: RunSummary[], nextCursor: string | null = null): RunPage {
  return { runs, nextCursor };
}

beforeEach(() => {
  api.get.mockResolvedValue(workflow());
  api.listForWorkflow.mockResolvedValue(page([FINISHED]));
  api.run.mockResolvedValue({ runId: "r-2" });
  api.stop.mockResolvedValue(undefined);
  api.runGet.mockReset();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("WorkflowSummary", () => {
  it("shows the new run straight away after Run, and offers Stop", async () => {
    render(<WorkflowSummary workflowId="wf-1" />);
    await screen.findByText("Morning digest");
    await screen.findByText("Succeeded");

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    expect(await screen.findByText("Running")).toBeTruthy();
    expect(api.run).toHaveBeenCalledWith("wf-1");
    expect(await screen.findByRole("button", { name: "Stop" })).toBeTruthy();
    // Once for the first load, once after the run started.
    expect(api.listForWorkflow).toHaveBeenCalledTimes(2);
  });

  it("does not offer Run for a workflow that starts from a chat message", async () => {
    api.get.mockResolvedValue(
      workflow({
        nodes: [
          { id: "t", type: "trigger", template: "chat", x: 0, y: 0 },
          { id: "p", type: "provider", x: 0, y: 0 },
        ],
      }),
    );
    render(<WorkflowSummary workflowId="wf-1" />);

    const button = await screen.findByRole("button", { name: "Run" });
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(true),
    );
    expect(
      screen.getByText(/run it from the AgentMesh desktop app/),
    ).toBeTruthy();
  });

  it("explains a workflow that is not deployed", async () => {
    api.get.mockResolvedValue(workflow({ status: "draft" }));
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(
      await screen.findByText(
        "Not deployed yet — finish and deploy it in the AgentMesh desktop app",
      ),
    ).toBeTruthy();
  });

  it("labels a paused workflow and says where to resume it", async () => {
    api.get.mockResolvedValue(workflow({ status: "paused" }));
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(
      await screen.findByText(
        "Paused — resume it in the AgentMesh desktop app",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Paused")).toBeTruthy();
  });

  it("says so when the server has no run history", async () => {
    api.listForWorkflow.mockRejectedValue(new api.RunsUnavailableError());
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(
      await screen.findByText(
        "Run history is not available on this server yet.",
      ),
    ).toBeTruthy();
  });

  it("opens a run's sheet when its row is tapped", async () => {
    render(<WorkflowSummary workflowId="wf-1" />);
    fireEvent.click((await screen.findByText("Succeeded")).closest("button")!);
    expect(screen.getByRole("dialog").textContent).toBe("sheet for r-1");
  });

  it("appends older runs without losing the ones already shown", async () => {
    const older: RunSummary = {
      ...FINISHED,
      id: "r-0",
      status: "failed",
      startedAt: new Date(Date.now() - 120_000).toISOString(),
    };
    api.listForWorkflow
      .mockResolvedValueOnce(page([FINISHED], "c1"))
      .mockResolvedValueOnce(page([older]));
    render(<WorkflowSummary workflowId="wf-1" />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Show older runs" }),
    );

    expect(await screen.findByText("Failed")).toBeTruthy();
    expect(screen.getByText("Succeeded")).toBeTruthy();
    expect(api.listForWorkflow).toHaveBeenLastCalledWith("wf-1", {
      cursor: "c1",
      limit: 20,
    });
    expect(
      screen.queryByRole("button", { name: "Show older runs" }),
    ).toBeNull();
  });

  it("picks up a run started elsewhere without a pull", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    try {
      api.listForWorkflow
        .mockResolvedValueOnce(page([FINISHED]))
        .mockResolvedValue(
          page([
            {
              ...FINISHED,
              id: "r-2",
              triggeredBy: "manual",
              status: "running",
              startedAt: new Date().toISOString(),
              finishedAt: undefined,
            },
            FINISHED,
          ]),
        );
      render(<WorkflowSummary workflowId="wf-1" />);
      await screen.findByText("Succeeded");

      await act(async () => {
        vi.advanceTimersByTime(10_000);
      });

      expect(await screen.findByText("Running")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("refreshes on coming back to the foreground", async () => {
    api.listForWorkflow
      .mockResolvedValueOnce(page([FINISHED]))
      .mockResolvedValue(
        page([{ ...FINISHED, id: "r-2", status: "failed" }, FINISHED]),
      );
    render(<WorkflowSummary workflowId="wf-1" />);
    await screen.findByText("Succeeded");

    document.dispatchEvent(new Event("visibilitychange"));

    expect(await screen.findByText("Failed")).toBeTruthy();
  });
});
