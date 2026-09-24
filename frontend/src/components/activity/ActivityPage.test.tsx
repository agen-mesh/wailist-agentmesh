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
  return { RunsUnavailableError, recent: vi.fn(), get: vi.fn() };
});

vi.mock("@/lib/api", () => ({
  RunsUnavailableError: api.RunsUnavailableError,
  runs: { recent: api.recent, get: api.get },
}));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/components/runs/UpcomingRuns", () => ({
  UpcomingRuns: () => null,
}));
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
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((r, j) => {
    resolve = r;
    reject = j;
  });
  return { promise, resolve, reject };
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

  // A run started somewhere else, such as on the website, while this screen
  // is open and nothing on it is running.
  it("picks up a run started elsewhere without a pull", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    try {
      api.recent
        .mockResolvedValueOnce(page([run({ id: "r-1" })]))
        .mockResolvedValue(
          page([
            run({
              id: "r-2",
              workflowName: "Started on the website",
              triggeredBy: "manual",
              status: "running",
              finishedAt: undefined,
            }),
            run({ id: "r-1" }),
          ]),
        );
      render(<ActivityPage />);
      await screen.findByText("Morning digest");

      await act(async () => {
        vi.advanceTimersByTime(10_000);
      });

      expect(await screen.findByText("Started on the website")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  // The first load is slow, a poll lands first, and the first load's older
  // answer must not replace it.
  it("drops a slow first load that a poll has overtaken", async () => {
    const first = deferred<RunPage>();
    api.recent
      .mockReturnValueOnce(first.promise)
      .mockResolvedValue(
        page([run({ id: "r-1", workflowName: "Newer answer" })]),
      );
    render(<ActivityPage />);

    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(await screen.findByText("Newer answer")).toBeTruthy();

    await act(async () => {
      first.resolve(page([run({ id: "r-1", workflowName: "Older answer" })]));
    });
    expect(screen.getByText("Newer answer")).toBeTruthy();
    expect(screen.queryByText("Older answer")).toBeNull();
  });

  // A failed first load shows an error; the next successful poll has to take
  // it away, not leave it standing above rows that loaded fine.
  it("clears the error when a later poll succeeds", async () => {
    api.recent
      .mockRejectedValueOnce(new Error("Could not reach the server"))
      .mockResolvedValue(page([run({ id: "r-1" })]));
    render(<ActivityPage />);
    expect(await screen.findByText("Could not reach the server")).toBeTruthy();

    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });

    expect(await screen.findByText("Morning digest")).toBeTruthy();
    expect(screen.queryByText("Could not reach the server")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("refreshes on coming back to the foreground", async () => {
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-1" })]))
      .mockResolvedValue(
        page([
          run({ id: "r-2", workflowName: "Ran while away" }),
          run({ id: "r-1" }),
        ]),
      );
    render(<ActivityPage />);
    await screen.findByText("Morning digest");

    document.dispatchEvent(new Event("visibilitychange"));

    expect(await screen.findByText("Ran while away")).toBeTruthy();
  });

  it("recovers once the server gains run history", async () => {
    api.recent
      .mockRejectedValueOnce(new api.RunsUnavailableError())
      .mockResolvedValue(page([run({ id: "r-1" })]));
    render(<ActivityPage />);
    await screen.findByText("Run history is not available on this server yet.");

    document.dispatchEvent(new Event("visibilitychange"));

    expect(await screen.findByText("Morning digest")).toBeTruthy();
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

  it("polls spend while an open run is running, then switches to idle polling", async () => {
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
      )
      .mockResolvedValue(
        page([run({ id: "r-1", status: "success", spendUsdMicros: 120_000 })]),
      );

    render(<ActivityPage />);
    await act(async () => {});
    fireEvent.click(screen.getByText("Morning digest").closest("button")!);
    expect(screen.getByRole("dialog").dataset.spend).toBe("21000");

    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(screen.getByRole("dialog").dataset.spend).toBe("90000");

    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(screen.getByRole("dialog").dataset.spend).toBe("120000");
    expect(screen.getByRole("dialog").dataset.status).toBe("success");

    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(api.recent).toHaveBeenCalledTimes(3);

    await act(async () => vi.advanceTimersByTimeAsync(6_000));
    expect(api.recent).toHaveBeenCalledTimes(3);

    await act(async () => vi.advanceTimersByTimeAsync(1_000));
    expect(api.recent).toHaveBeenCalledTimes(4);
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

  it("drops an older refresh after a newer poll has landed", async () => {
    const refresh = deferred<RunPage>();
    const poll = deferred<RunPage>();
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-1" })]))
      .mockReturnValueOnce(refresh.promise)
      .mockReturnValueOnce(poll.promise);
    render(<ActivityPage />);
    await screen.findByText("Morning digest");

    fireEvent.click(screen.getByRole("button", { name: "Pull to refresh" }));
    document.dispatchEvent(new Event("visibilitychange"));

    await act(async () => {
      poll.resolve(
        page([run({ id: "r-2", workflowName: "Newer poll result" })]),
      );
    });
    expect(screen.getByText("Newer poll result")).toBeTruthy();

    await act(async () => {
      refresh.resolve(
        page([run({ id: "r-3", workflowName: "Older refresh result" })]),
      );
    });
    expect(screen.getByText("Newer poll result")).toBeTruthy();
    expect(screen.queryByText("Older refresh result")).toBeNull();
  });

  it("accepts a pending refresh after a newer poll fails", async () => {
    const refresh = deferred<RunPage>();
    const poll = deferred<RunPage>();
    api.recent
      .mockResolvedValueOnce(page([run({ id: "r-1" })]))
      .mockReturnValueOnce(refresh.promise)
      .mockReturnValueOnce(poll.promise);
    render(<ActivityPage />);
    await screen.findByText("Morning digest");

    fireEvent.click(screen.getByRole("button", { name: "Pull to refresh" }));
    document.dispatchEvent(new Event("visibilitychange"));

    await act(async () => {
      poll.reject(new Error("offline"));
    });
    await act(async () => {
      refresh.resolve(
        page([run({ id: "r-2", workflowName: "Refreshed result" })]),
      );
    });
    expect(screen.getByText("Refreshed result")).toBeTruthy();
  });

  describe("while a run is going", () => {
    afterEach(() => vi.useRealTimers());

    // A slow poll overtaken by a newer one could land last and put a finished
    // run back to running.
    it("polls one request at a time", async () => {
      vi.useFakeTimers();
      const slow = deferred<RunPage>();
      api.recent
        .mockResolvedValueOnce(page([run({ id: "r-1", status: "running" })]))
        .mockReturnValueOnce(slow.promise);
      render(<ActivityPage />);
      await act(async () => {});

      await act(async () => vi.advanceTimersByTimeAsync(3_000));
      await act(async () => vi.advanceTimersByTimeAsync(9_000));
      expect(api.recent).toHaveBeenCalledTimes(2);

      await act(async () => {
        slow.resolve(page([run({ id: "r-1", status: "success" })]));
      });
      expect(screen.queryByText("Running")).toBeNull();
    });

    it("updates a running run that newer runs pushed off the first page", async () => {
      vi.useFakeTimers();
      const newer = Array.from({ length: 20 }, (_, i) =>
        run({ id: `n-${i}`, workflowName: `Newer ${i}` }),
      );
      api.recent
        .mockResolvedValueOnce(
          page([
            run({ id: "r-1", status: "running", workflowName: "Long job" }),
          ]),
        )
        .mockResolvedValueOnce(page(newer));
      api.get.mockResolvedValue({
        run: {
          id: "r-1",
          workflowId: "wf-1",
          triggeredBy: "schedule",
          status: "success",
          startedAt: new Date().toISOString(),
          finishedAt: new Date().toISOString(),
          spendUsdMicros: 80_000,
        },
        logs: [],
        deadLetters: [],
      });
      render(<ActivityPage />);
      await act(async () => {});
      expect(screen.getByText("Running")).toBeTruthy();

      await act(async () => vi.advanceTimersByTimeAsync(3_000));
      expect(api.get).toHaveBeenCalledWith("r-1");
      const row = screen.getByText("Long job").closest("button")!;
      expect(row.textContent).not.toContain("Running");
      expect(row.textContent).toContain("$0.08");
    });
  });
});
