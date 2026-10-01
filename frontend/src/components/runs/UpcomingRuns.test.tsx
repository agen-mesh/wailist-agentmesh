import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { UpcomingRun } from "@/lib/types";

const api = vi.hoisted(() => {
  class UpcomingUnavailableError extends Error {}
  return { UpcomingUnavailableError, upcoming: vi.fn() };
});

vi.mock("@/lib/api", () => ({
  UpcomingUnavailableError: api.UpcomingUnavailableError,
  schedules: { upcoming: api.upcoming },
}));

import { UpcomingRuns } from "./UpcomingRuns";

const HOUR = 3_600_000;
const at = (ms: number) => new Date(Date.now() + ms).toISOString();

function run(overrides: Partial<UpcomingRun>): UpcomingRun {
  return {
    workflowId: "wf-brief",
    workflowName: "Daily Market Brief",
    at: at(HOUR),
    cron: "0 9 * * *",
    ...overrides,
  };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  // Midday, so "in 10 minutes" is today and "in 26 hours" is tomorrow
  // whenever the suite runs. Only Date is faked; timers stay real.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(2026, 8, 19, 12, 0, 0));
  api.upcoming.mockResolvedValue([
    run({
      workflowId: "wf-watch",
      workflowName: "Compliance Watch",
      at: at(10 * 60_000),
    }),
    run({ at: at(26 * HOUR) }),
  ]);
});

afterEach(() => {
  vi.useRealTimers();
  cleanup();
  vi.clearAllMocks();
});

describe("UpcomingRuns", () => {
  it("shows only the next two occurrences in the overview, including repeats", async () => {
    const name = "A long workflow name that remains available when the visible text is truncated";
    api.upcoming.mockResolvedValue([
      run({ workflowName: name, at: at(HOUR) }),
      run({ workflowName: name, at: at(2 * HOUR) }),
      run({ workflowId: "wf-later", workflowName: "Later workflow", at: at(26 * HOUR) }),
    ]);
    render(<UpcomingRuns title="Upcoming runs" limit={2} presentation="overview" />);
    const rows = await screen.findAllByRole("link");
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row.getAttribute("title") === name)).toBe(true);
    expect(rows.every((row) => row.getAttribute("href")?.includes("wf-brief"))).toBe(true);
    expect(rows[0].textContent).toContain("Today");
    expect(rows[0].querySelector("time")?.dateTime).toBe(at(HOUR));
    expect(screen.queryByText("Later workflow")).toBeNull();
    expect(api.upcoming).toHaveBeenCalledWith({ limit: 2, per: 3 });
  });

  it("keeps overview loading distinct from an empty schedule", async () => {
    let resolve!: (runs: UpcomingRun[]) => void;
    api.upcoming.mockReturnValue(new Promise<UpcomingRun[]>((r) => { resolve = r; }));
    render(<UpcomingRuns limit={2} presentation="overview" />);
    expect(screen.getByRole("status").textContent).toContain("Loading scheduled runs");
    expect(screen.queryByText("Nothing scheduled.")).toBeNull();
    await act(async () => resolve([]));
    expect(screen.getByText("Nothing scheduled.")).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("retries an overview load error without losing the section", async () => {
    api.upcoming.mockRejectedValueOnce(new Error("500")).mockResolvedValue([run({})]);
    render(<UpcomingRuns limit={2} presentation="overview" />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("link")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("lists what runs next, soonest first, grouped by day", async () => {
    render(<UpcomingRuns />);
    const rows = await screen.findAllByRole("link");
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining("Compliance Watch"),
      expect.stringContaining("Daily Market Brief"),
    ]);
    expect(rows[0].textContent).toContain("in 10 min");
    expect(rows[0].getAttribute("href")).toContain("wf-watch");
    expect(screen.getByText("Today")).toBeTruthy();
    expect(screen.getByText("Tomorrow")).toBeTruthy();
    expect(api.upcoming).toHaveBeenCalledWith({ limit: 5, per: 3 });
  });

  it("narrows to one workflow and drops the name and links", async () => {
    render(<UpcomingRuns workflowId="wf-brief" limit={3} />);
    await screen.findByText("Upcoming");
    expect(screen.queryByText("Compliance Watch")).toBeNull();
    expect(screen.queryByText("Daily Market Brief")).toBeNull();
    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(api.upcoming).toHaveBeenCalledWith({
      limit: 3,
      per: 3,
      workflowId: "wf-brief",
    });
  });

  it("says when nothing is scheduled, unless told to hide", async () => {
    api.upcoming.mockResolvedValue([]);
    const { unmount } = render(<UpcomingRuns />);
    expect(await screen.findByText("Nothing scheduled.")).toBeTruthy();
    unmount();

    render(<UpcomingRuns hideWhenEmpty />);
    await settle();
    expect(screen.queryByText("Upcoming")).toBeNull();
  });

  it("renders nothing on a server without the route", async () => {
    api.upcoming.mockRejectedValue(new api.UpcomingUnavailableError());
    render(<UpcomingRuns />);
    await settle();
    expect(screen.queryByText("Upcoming")).toBeNull();
  });

  // Every production caller hides an empty list, so an error that became []
  // made the section vanish as if nothing were scheduled.
  it("says a failed load failed, even where empty is hidden", async () => {
    api.upcoming
      .mockRejectedValueOnce(new Error("500"))
      .mockResolvedValue([run({})]);
    render(<UpcomingRuns hideWhenEmpty />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText(/Couldn.t load upcoming runs/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Daily Market Brief")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("drops an older list that lands after a newer one", async () => {
    let settleFirst!: (v: UpcomingRun[]) => void;
    api.upcoming
      .mockReturnValueOnce(
        new Promise<UpcomingRun[]>((r) => {
          settleFirst = r;
        }),
      )
      .mockResolvedValue([run({ workflowName: "Newer schedule" })]);
    render(<UpcomingRuns />);

    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(await screen.findByText("Newer schedule")).toBeTruthy();

    await act(async () => {
      settleFirst([run({ workflowName: "Older schedule" })]);
    });
    expect(screen.getByText("Newer schedule")).toBeTruthy();
    expect(screen.queryByText("Older schedule")).toBeNull();
  });
});
