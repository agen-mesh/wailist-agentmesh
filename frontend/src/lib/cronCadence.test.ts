import { describe, expect, it } from "vitest";

// Pin a fixed, non-UTC timezone with a stable (non-DST-ambiguous) offset for
// the whole file, set before any Date is constructed below — Node reads
// process.env.TZ when resolving the local timezone for Date's local
// accessors, so this makes every test deterministic regardless of which
// machine/CI runner executes it. America/New_York in January is a fixed
// UTC-5 (EST, no DST), avoiding a DST-transition-week false failure.
//
// This must be a plain top-level statement, not a beforeAll(...) callback:
// beforeAll's body doesn't run until vitest actually executes lifecycle
// hooks, which happens AFTER all of this module's top-level code (including
// the NOW constant below) has already run during module collection. A
// beforeAll here would pin TZ too late to affect NOW's underlying instant —
// only a synchronous statement, in file order, actually runs first.
process.env.TZ = "America/New_York";

import {
  cadenceToCron,
  cronToCadence,
  describeCadence,
  nextLocalRun,
} from "./cronCadence";

// A fixed "now" anchor: Wednesday, Jan 14 2026, in whatever timezone the
// test runner's Date resolves TZ to (America/New_York, set above).
const NOW = new Date(2026, 0, 14, 12, 0, 0);

describe("cadenceToCron", () => {
  it("converts a daily local time to UTC minute/hour, no day fields", () => {
    // 09:00 EST (UTC-5) -> 14:00 UTC
    expect(cadenceToCron({ cadence: "daily", time: "09:00" }, NOW)).toBe(
      "0 14 * * *",
    );
  });

  it("converts a weekly local time+day to UTC minute/hour/day-of-week", () => {
    // Monday (1) 09:00 EST -> Monday 14:00 UTC, same day (no rollover at 9am)
    expect(
      cadenceToCron({ cadence: "weekly", time: "09:00", dayOfWeek: 1 }, NOW),
    ).toBe("0 14 * * 1");
  });

  it("rolls the day-of-week forward when local time crosses midnight UTC", () => {
    // Sunday (0) 21:00 EST = 02:00 UTC the next day (Monday) -> dow rolls 0 -> 1
    expect(
      cadenceToCron({ cadence: "weekly", time: "21:00", dayOfWeek: 0 }, NOW),
    ).toBe("0 2 * * 1");
  });

  it("converts a monthly local time+day to UTC minute/hour/day-of-month", () => {
    expect(
      cadenceToCron({ cadence: "monthly", time: "09:00", dayOfMonth: 15 }, NOW),
    ).toBe("0 14 15 * *");
  });

  it("keeps a day that moves to the next UTC day, when that day exists in every month", () => {
    // Day 15 at 21:00 EST is 02:00 UTC on the 16th, every month.
    expect(
      cadenceToCron({ cadence: "monthly", time: "21:00", dayOfMonth: 15 }, NOW),
    ).toBe("0 2 16 * *");
    // Day 27 at 21:00 EST is the 28th in UTC, which every month has.
    expect(
      cadenceToCron({ cadence: "monthly", time: "21:00", dayOfMonth: 27 }, NOW),
    ).toBe("0 2 28 * *");
  });

  // Day 28 at 21:00 EST is 02:00 UTC on the 29th. The old conversion saved
  // "0 2 29 * *", which never fires in a non-leap February, and in February
  // itself fell back to the 28th in UTC, which fires on the 27th locally.
  // A standard cron cannot say "the day after the 28th", so it is refused.
  it("refuses a monthly time that lands on UTC day 29 or later", () => {
    expect(() =>
      cadenceToCron({ cadence: "monthly", time: "21:00", dayOfMonth: 28 }, NOW),
    ).toThrow(/different time or day/);
    const febNow = new Date(2026, 1, 10, 12, 0, 0);
    expect(() =>
      cadenceToCron({ cadence: "monthly", time: "21:00", dayOfMonth: 28 }, febNow),
    ).toThrow(/different time or day/);
  });

  // 19:30 in New York is 00:30 UTC the next day in winter (EST) but 23:30
  // UTC the same day in summer (EDT). No single UTC day-of-month is right
  // all year, and the answer must not depend on the month it was saved in.
  it("refuses a monthly time whose UTC day changes with daylight saving", () => {
    expect(() =>
      cadenceToCron({ cadence: "monthly", time: "19:30", dayOfMonth: 10 }, NOW),
    ).toThrow(/different time or day/);
    const julyNow = new Date(2026, 6, 10, 12, 0, 0);
    expect(() =>
      cadenceToCron({ cadence: "monthly", time: "19:30", dayOfMonth: 10 }, julyNow),
    ).toThrow(/different time or day/);
  });

  it("gives the same day whatever month it is saved in", () => {
    const days = Array.from({ length: 12 }, (_, m) =>
      cadenceToCron(
        { cadence: "monthly", time: "09:00", dayOfMonth: 20 },
        new Date(2026, m, 5, 12, 0, 0),
      ).split(" ")[2],
    );
    expect(new Set(days)).toEqual(new Set(["20"]));
  });
});

describe("cronToCadence", () => {
  it("round-trips a daily cron back to local time", () => {
    const cron = cadenceToCron({ cadence: "daily", time: "09:00" }, NOW);
    expect(cronToCadence(cron, NOW)).toEqual({
      cadence: "daily",
      time: "09:00",
    });
  });

  it("round-trips a weekly cron back to local time+day", () => {
    const cron = cadenceToCron(
      { cadence: "weekly", time: "09:00", dayOfWeek: 3 },
      NOW,
    );
    expect(cronToCadence(cron, NOW)).toEqual({
      cadence: "weekly",
      time: "09:00",
      dayOfWeek: 3,
    });
  });

  it("round-trips a monthly cron back to local time+day", () => {
    const cron = cadenceToCron(
      { cadence: "monthly", time: "09:00", dayOfMonth: 10 },
      NOW,
    );
    expect(cronToCadence(cron, NOW)).toEqual({
      cadence: "monthly",
      time: "09:00",
      dayOfMonth: 10,
    });
  });

  it("returns null for a cron this UI never produces (explicit month field)", () => {
    expect(cronToCadence("0 14 1 6 *", NOW)).toBeNull();
  });

  it("returns null for a malformed expression", () => {
    expect(cronToCadence("not a cron", NOW)).toBeNull();
  });

  it("returns null for a day-of-month outside 1-28, rather than clamping to a fabricated day", () => {
    // day 30, 12:00 UTC -> 07:00 EST, same calendar day -- a real day-30
    // schedule this UI could never have set (the picker only offers 1-28),
    // so it must be rejected, not silently reported as some other day.
    expect(cronToCadence("0 12 30 * *", NOW)).toBeNull();
  });
});

describe("describeCadence", () => {
  it("summarises each cadence in plain English", () => {
    expect(describeCadence({ cadence: "daily", time: "09:00" })).toBe(
      "Every day at 9:00 AM",
    );
    expect(
      describeCadence({ cadence: "weekly", time: "13:30", dayOfWeek: 1 }),
    ).toBe("Every Monday at 1:30 PM");
    expect(
      describeCadence({ cadence: "monthly", time: "00:05", dayOfMonth: 22 }),
    ).toBe("The 22nd of every month at 12:05 AM");
  });
});

describe("nextLocalRun", () => {
  // NOW is Wednesday, Jan 14 2026, 12:00 local.
  it("daily: today if the time is still ahead, else tomorrow", () => {
    expect(nextLocalRun({ cadence: "daily", time: "15:00" }, NOW)).toEqual(
      new Date(2026, 0, 14, 15, 0),
    );
    expect(nextLocalRun({ cadence: "daily", time: "09:00" }, NOW)).toEqual(
      new Date(2026, 0, 15, 9, 0),
    );
  });

  it("weekly: same weekday rolls a full week once the time has passed", () => {
    expect(
      nextLocalRun({ cadence: "weekly", time: "09:00", dayOfWeek: 3 }, NOW),
    ).toEqual(new Date(2026, 0, 21, 9, 0));
    expect(
      nextLocalRun({ cadence: "weekly", time: "09:00", dayOfWeek: 1 }, NOW),
    ).toEqual(new Date(2026, 0, 19, 9, 0));
  });

  it("monthly: rolls into next month once the day has passed", () => {
    expect(
      nextLocalRun({ cadence: "monthly", time: "09:00", dayOfMonth: 20 }, NOW),
    ).toEqual(new Date(2026, 0, 20, 9, 0));
    expect(
      nextLocalRun({ cadence: "monthly", time: "09:00", dayOfMonth: 1 }, NOW),
    ).toEqual(new Date(2026, 1, 1, 9, 0));
  });
});
