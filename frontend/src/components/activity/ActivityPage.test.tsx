import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
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
  PullToRefresh: ({
    children,
    onRefresh,
  }: {
    children: React.ReactNode;
    onRefresh: () => Promise<unknown>;
  }) => (
    <div>
      <button type="button" onClick={() => void onRefresh()}>
        Pull to refresh
      </button>
      {children}
    </div>
  ),
}));
vi.mock("@/components/runs/RunSheet", () => ({
  RunSheet: ({ run }: { run: RunSummary }) => (
    <div
      role="dialog"
      data-spend={run.spendUsdMicros}
      data-status={run.status}
    >
      sheet for {run.id}
    </div>
  ),
}));

// A promise the test settles when it chooses, to put responses out of order.
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

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
  vi.useRealTimers();
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

  // The sheet shows the row as it is now, not as it was when tapped.
  it("updates an open sheet when a refresh brings new figures", async () => {
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-1", status: "running" })]))
      .mockResolvedValueOnce(
        page([run({ id: "r-1", status: "running", spendUsdMicros: 90_000 })]),
      );
    render(<ActivityPage />);
    fireEvent.click(
      (await screen.findByText("Morning digest")).closest("button")!,
    );
    expect(screen.getByRole("dialog").dataset.spend).toBe("21000");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Pull to refresh" }));
    });
    expect(screen.getByRole("dialog").dataset.spend).toBe("90000");
  });

  it("polls spend while an open run is running, then stops", async () => {
    vi.useFakeTimers();
    api.recent
      .mockResolvedValueOnce(
        page([run({ id: "r-1", status: "running", spendUsdMicros: 21_000 })]),
      )
      .mockResolvedValueOnce(
        page([run({ id: "r-1", status: "running", spendUsdMicros: 90_000 })]),
      )
      .mockResolvedValueOnce(
        page([run({ id: "r-1", status: "success", spendUsdMicros: 120_000 })]),
      );

    render(<ActivityPage />);
    await act(async () => {});
    fireEvent.click(screen.getByText("Morning digest").closest("button")!);
    expect(screen.getByRole("dialog").dataset.spend).toBe("21000");

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(screen.getByRole("dialog").dataset.spend).toBe("90000");

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(screen.getByRole("dialog").dataset.spend).toBe("120000");
    expect(screen.getByRole("dialog").dataset.status).toBe("success");

    await act(async () => vi.advanceTimersByTimeAsync(10_000));
    expect(api.recent).toHaveBeenCalledTimes(3);
  });

  it("drops an older page that lands after a refresh", async () => {
    const older = deferred<RunPage>();
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-3" })], "c1"))
      .mockReturnValueOnce(older.promise)
      .mockResolvedValueOnce(
        page([run({ id: "r-4", workflowName: "Fresh run" })], "c9"),
      );
    render(<ActivityPage />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Show older runs" }),
    );
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Pull to refresh" }));
    });
    expect(screen.getByText("Fresh run")).toBeTruthy();

    await act(async () => {
      older.resolve(page([run({ id: "r-1", workflowName: "Stale page" })]));
    });
    expect(screen.queryByText("Stale page")).toBeNull();
    // The cursor is still the refreshed list's.
    fireEvent.click(screen.getByRole("button", { name: "Show older runs" }));
    expect(api.recent).toHaveBeenLastCalledWith({ cursor: "c9", limit: 20 });
  });

  it("drops a slow first load that lands after a refresh", async () => {
    const first = deferred<RunPage>();
    api.recent
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(
        page([run({ id: "r-4", workflowName: "Fresh run" })]),
      );
    render(<ActivityPage />);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Pull to refresh" }));
    });
    await act(async () => {
      first.resolve(page([run({ id: "r-1", workflowName: "Stale load" })]));
    });
    expect(screen.getByText("Fresh run")).toBeTruthy();
    expect(screen.queryByText("Stale load")).toBeNull();
  });
});
