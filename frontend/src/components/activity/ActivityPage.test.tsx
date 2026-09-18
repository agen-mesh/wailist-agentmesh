import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { RunPage, RunSummary } from "@/lib/types";

// The screen is tested against a stubbed API. The top bar, the pull gesture
// and the sheet have their own tests, so they are reduced to what this screen
// hands them.
const api = vi.hoisted(() => {
  class RunsUnavailableError extends Error {}
  return { RunsUnavailableError, recent: vi.fn() };
});

vi.mock("@/lib/api", () => ({
  RunsUnavailableError: api.RunsUnavailableError,
  runs: { recent: api.recent },
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

import { ActivityPage } from "./ActivityPage";

const DAY_MS = 86_400_000;

function run(overrides: Partial<RunSummary>): RunSummary {
  return {
    id: "r-1",
    workflowId: "wf-1",
    workflowName: "Morning digest",
    triggeredBy: "schedule",
    status: "success",
    startedAt: new Date().toISOString(),
    finishedAt: new Date().toISOString(),
    spendUsdMicros: 21_000,
    ...overrides,
  };
}

function page(runs: RunSummary[], nextCursor: string | null = null): RunPage {
  return { runs, nextCursor };
}

beforeEach(() => {
  api.recent.mockResolvedValue(
    page([
      run({ id: "r-2", workflowName: "Morning digest" }),
      run({
        id: "r-1",
        workflowName: "Invoice check",
        status: "failed",
        startedAt: new Date(Date.now() - 3 * DAY_MS).toISOString(),
        finishedAt: undefined,
      }),
    ]),
  );
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ActivityPage", () => {
  it("lists runs across workflows under the day they ran", async () => {
    render(<ActivityPage />);

    const today = await screen.findByRole("region", { name: "Today" });
    expect(today.textContent).toContain("Morning digest");
    expect(today.textContent).not.toContain("Invoice check");
    expect(screen.getByText("Invoice check")).toBeTruthy();
    expect(screen.getByText("Failed")).toBeTruthy();
    expect(api.recent).toHaveBeenCalledWith({ limit: 20 });
  });

  it("opens a run's sheet when its row is tapped", async () => {
    render(<ActivityPage />);
    fireEvent.click(
      (await screen.findByText("Invoice check")).closest("button")!,
    );
    expect(screen.getByRole("dialog").textContent).toBe("sheet for r-1");
  });

  it("says so when the server has no run history", async () => {
    api.recent.mockRejectedValue(new api.RunsUnavailableError());
    render(<ActivityPage />);
    expect(
      await screen.findByText(
        "Run history is not available on this server yet.",
      ),
    ).toBeTruthy();
  });

  it("says when nothing has run yet", async () => {
    api.recent.mockResolvedValue(page([]));
    render(<ActivityPage />);
    expect(await screen.findByText(/No runs yet/)).toBeTruthy();
  });

  it("appends older runs from the cursor", async () => {
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-2" })], "c1"))
      .mockResolvedValueOnce(
        page([run({ id: "r-1", workflowName: "Invoice check" })]),
      );
    render(<ActivityPage />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Show older runs" }),
    );

    expect(await screen.findByText("Invoice check")).toBeTruthy();
    expect(screen.getByText("Morning digest")).toBeTruthy();
    expect(api.recent).toHaveBeenLastCalledWith({ cursor: "c1", limit: 20 });
    expect(
      screen.queryByRole("button", { name: "Show older runs" }),
    ).toBeNull();
  });
});
