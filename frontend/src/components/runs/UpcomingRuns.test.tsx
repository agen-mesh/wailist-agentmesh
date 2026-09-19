import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
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
    expect(api.upcoming).toHaveBeenCalledWith({ limit: 50, per: 3 });
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
});
