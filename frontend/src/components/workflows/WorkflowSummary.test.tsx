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
  // A scheduled workflow also shows its upcoming runs, which have their own
  // tests; here they only need to answer.
  UpcomingUnavailableError: class extends Error {},
  schedules: { upcoming: vi.fn(async () => []) },
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
import { describeSchedule } from "@/lib/describeSchedule";

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
  // A poll asks runs.get about a run the list has not picked up yet, so it
  // needs a promise back. Still running: the pending row stays as it is.
  api.runGet.mockResolvedValue({
    run: {
      id: "r-2",
      workflowId: "wf-1",
      triggeredBy: "manual",
      status: "running",
      startedAt: new Date().toISOString(),
    },
    logs: [],
    deadLetters: [],
  });
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

  // The cron is for the scheduler. A person gets it in words, in their time.
  it("says its schedule in words, not as a cron expression", async () => {
    api.get.mockResolvedValue(workflow({ scheduleCron: "0 7 * * 1-5" }));
    const { container } = render(<WorkflowSummary workflowId="wf-1" />);
    const line = await screen.findByText(describeSchedule("0 7 * * 1-5"));
    expect(line.textContent).toMatch(/^Every weekday at /);
    expect(line.getAttribute("title")).toBe("0 7 * * 1-5");
    expect(container.textContent).not.toContain("0 7 * * 1-5");
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

  // The figures come with the workflow, which used to be read once, so they
  // stayed at their pre-run values while the run list moved on.
  // Every read of the workflow is numbered. The read triggered when a run
  // starts can be slow and land after the one triggered when it finishes; it
  // must not put the older figures back.
  it("keeps the newest figures when an older workflow read lands last", async () => {
    let settleStart!: (wf: Workflow) => void;
    api.get
      .mockResolvedValueOnce(workflow({ totalRuns: 1 }))
      .mockReturnValueOnce(
        new Promise<Workflow>((r) => {
          settleStart = r;
        }),
      )
      .mockResolvedValue(workflow({ totalRuns: 3 }));
    render(<WorkflowSummary workflowId="wf-1" />);
    await screen.findByText("Succeeded");
    const fact = (label: string) =>
      screen.queryByText(label)?.nextElementSibling?.textContent;

    const started = {
      ...FINISHED,
      id: "r-2",
      status: "running" as const,
      startedAt: new Date().toISOString(),
      finishedAt: undefined,
    };
    api.listForWorkflow.mockResolvedValue(page([started, FINISHED]));
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2));

    api.listForWorkflow.mockResolvedValue(
      page([{ ...started, status: "success" }, FINISHED]),
    );
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await waitFor(() => expect(fact("Total runs")).toBe("3"));

    await act(async () => {
      settleStart(workflow({ totalRuns: 2 }));
    });
    expect(fact("Total runs")).toBe("3");
  });

  // The quiet re-read a run starting triggers has no error handler: failing
  // keeps what is shown. It used to count as "newer" all the same, so a first
  // load still in flight was thrown away when it landed after that failure,
  // and the screen stayed on its skeleton. Only a read that has succeeded
  // outranks an older one.
  it("still shows the first load when a newer quiet read fails first", async () => {
    let settleFirst!: (wf: Workflow) => void;
    api.get
      .mockReturnValueOnce(
        new Promise<Workflow>((r) => {
          settleFirst = r;
        }),
      )
      .mockRejectedValueOnce(new Error("offline"));
    render(<WorkflowSummary workflowId="wf-1" />);
    await waitFor(() => expect(api.listForWorkflow).toHaveBeenCalledTimes(1));

    const started = {
      ...FINISHED,
      id: "r-2",
      status: "running" as const,
      startedAt: new Date().toISOString(),
      finishedAt: undefined,
    };
    api.listForWorkflow.mockResolvedValue(page([started, FINISHED]));
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2));
    await act(async () => {});

    await act(async () => {
      settleFirst(workflow({ totalRuns: 1 }));
    });
    const fact = (label: string) =>
      screen.queryByText(label)?.nextElementSibling?.textContent;
    await waitFor(() => expect(fact("Total runs")).toBe("1"));
  });

  it("re-reads its figures when a run starts and when it finishes", async () => {
    render(<WorkflowSummary workflowId="wf-1" />);
    await screen.findByText("Succeeded");
    expect(api.get).toHaveBeenCalledTimes(1);

    api.get.mockResolvedValue(workflow({ totalRuns: 2 }));
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await screen.findByText("Running");
    await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2));

    api.get.mockResolvedValue(workflow({ totalRuns: 2, runs: 2 }));
    api.listForWorkflow.mockResolvedValue(
      page([{ ...FINISHED, id: "r-2" }, FINISHED]),
    );
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(api.get).toHaveBeenCalledTimes(3));
    const fact = (label: string) =>
      screen.getByText(label).nextElementSibling?.textContent;
    await waitFor(() => expect(fact("Total runs")).toBe("2"));
  });

  it("describes itself from its graph until a description is written", async () => {
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(await screen.findByText(/^Runs when started\./)).toBeTruthy();
    expect(screen.getByText("Summarised from its steps.")).toBeTruthy();
  });

  it("shows a written description as it is", async () => {
    api.get.mockResolvedValue(
      workflow({ description: "Sorts support email." }),
    );
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(await screen.findByText("Sorts support email.")).toBeTruthy();
    expect(screen.queryByText("Summarised from its steps.")).toBeNull();
  });

  // GetWorkflow leaves the 30-day pair at zero when its aggregation fails,
  // exactly as it does for a workflow that had no runs and no spend. Printed
  // as "0" and "$0.00" that is a figure the reader has no reason to doubt.
  it("shows dashes for the 30-day figures the server could not total", async () => {
    api.get.mockResolvedValue(
      workflow({
        statsUnavailable: true,
        totalRuns: 7,
        runs: 0,
        spend: undefined,
      }),
    );
    render(<WorkflowSummary workflowId="wf-1" />);
    await screen.findByText("Succeeded");
    const fact = (label: string) =>
      screen.getByText(label).nextElementSibling?.textContent;

    expect(fact("Runs · 30 days")).toBe("—");
    expect(fact("Spent · 30 days")).toBe("—");
    // totalRuns has its own nullable field and was answered, so it stands.
    expect(fact("Total runs")).toBe("7");
  });

  it("shows its run figures", async () => {
    api.get.mockResolvedValue(
      workflow({ totalRuns: 1842, runs: 38, spend: "1.482" }),
    );
    render(<WorkflowSummary workflowId="wf-1" />);
    const fact = async (label: string) =>
      (await screen.findByText(label)).nextElementSibling?.textContent;
    expect(await fact("Total runs")).toBe((1842).toLocaleString());
    expect(await fact("Runs · 30 days")).toBe("38");
    expect(await fact("Spent · 30 days")).toBe("$1.48");
    expect(await fact("Next run")).toBe("—");
  });

  it("lists its agents with the model and tools each uses", async () => {
    api.get.mockResolvedValue(
      workflow({
        nodes: [
          { id: "t", type: "trigger", template: "manual", x: 0, y: 0 },
          { id: "a", type: "agent", name: "Triage agent", x: 0, y: 0 },
          { id: "p", type: "provider", model: "gemini-2.5-flash", x: 0, y: 0 },
          { id: "h", type: "tool", template: "http", x: 0, y: 0 },
        ],
        edges: [
          { id: "e1", from: "p", to: "a", kind: "attach", toPort: "model" },
          { id: "e2", from: "h", to: "a", kind: "attach", toPort: "tools" },
        ],
      }),
    );
    render(<WorkflowSummary workflowId="wf-1" />);
    expect(await screen.findByText("Triage agent")).toBeTruthy();
    expect(screen.getByText("gemini-2.5-flash · 1 tool")).toBeTruthy();
  });

  it("shows status as a word beside a dot, not a pill", async () => {
    render(<WorkflowSummary workflowId="wf-1" />);
    const status = await screen.findByText("Deployed");
    expect(status.className).toBe("wfd-status");
  });
});
