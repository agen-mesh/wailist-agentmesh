import { describe, expect, it } from "vitest";

// Same pinning as cronCadence.test.ts: a plain top-level statement, before
// any Date exists, so every local-time accessor below is deterministic.
// America/New_York in January is a fixed UTC-5.
process.env.TZ = "America/New_York";

import {
  describeCadence,
  describeCron,
  describeNextRun,
  describeRun,
  formatLocalTime,
  nextLocalRun,
  observesDaylightSaving,
  ordinal,
  timeZoneLabel,
} from "./scheduleText";

// Wednesday, Jan 14 2026, 12:00 local.
const NOW = new Date(2026, 0, 14, 12, 0, 0);

describe("formatLocalTime", () => {
  it("renders a 12-hour clock", () => {
    expect(formatLocalTime("09:00")).toBe("9:00 AM");
    expect(formatLocalTime("00:05")).toBe("12:05 AM");
    expect(formatLocalTime("12:00")).toBe("12:00 PM");
    expect(formatLocalTime("23:59")).toBe("11:59 PM");
  });
});

describe("ordinal", () => {
  it("handles the teens and the 1/2/3 endings", () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 28].map(ordinal)).toEqual([
      "1st",
      "2nd",
      "3rd",
      "4th",
      "11th",
      "12th",
      "13th",
      "21st",
      "22nd",
      "23rd",
      "28th",
    ]);
  });
});

describe("describeCadence", () => {
  it("says each cadence in plain English", () => {
    expect(describeCadence({ cadence: "daily", time: "09:00" })).toBe(
      "Every day at 9:00 AM",
    );
    expect(
      describeCadence({ cadence: "weekly", time: "13:30", dayOfWeek: 1 }),
    ).toBe("Every Monday at 1:30 PM");
    expect(
      describeCadence({ cadence: "monthly", time: "00:05", dayOfMonth: 22 }),
    ).toBe("On the 22nd of every month at 12:05 AM");
  });
});

describe("describeCron", () => {
  it("reads a stored UTC cron back in local time", () => {
    // 14:00 UTC is 09:00 EST.
    expect(describeCron("0 14 * * *", NOW)).toBe("Every day at 9:00 AM");
    expect(describeCron("0 14 * * 1", NOW)).toBe("Every Monday at 9:00 AM");
    expect(describeCron("0 14 15 * *", NOW)).toBe(
      "On the 15th of every month at 9:00 AM",
    );
  });

  it("returns null for a shape the app never writes", () => {
    expect(describeCron("*/15 * * * *", NOW)).toBeNull();
    expect(describeCron("0 9 * 1 *", NOW)).toBeNull();
  });
});

describe("nextLocalRun", () => {
  it("daily: today while the time is still ahead, else tomorrow", () => {
    expect(nextLocalRun({ cadence: "daily", time: "15:00" }, NOW)).toEqual(
      new Date(2026, 0, 14, 15, 0),
    );
    expect(nextLocalRun({ cadence: "daily", time: "09:00" }, NOW)).toEqual(
      new Date(2026, 0, 15, 9, 0),
    );
  });

  it("weekly: the same weekday rolls a full week once its time has passed", () => {
    expect(
      nextLocalRun({ cadence: "weekly", time: "09:00", dayOfWeek: 3 }, NOW),
    ).toEqual(new Date(2026, 0, 21, 9, 0));
    expect(
      nextLocalRun({ cadence: "weekly", time: "15:00", dayOfWeek: 3 }, NOW),
    ).toEqual(new Date(2026, 0, 14, 15, 0));
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

describe("describeRun", () => {
  it("says today and tomorrow by name", () => {
    expect(describeRun(new Date(2026, 0, 14, 15, 0), NOW)).toBe(
      "Today at 3:00 PM (in 3 hours)",
    );
    expect(describeRun(new Date(2026, 0, 14, 12, 30), NOW)).toBe(
      "Today at 12:30 PM (in 30 minutes)",
    );
    expect(describeRun(new Date(2026, 0, 14, 12, 1), NOW)).toBe(
      "Today at 12:01 PM (in 1 minute)",
    );
    expect(describeRun(new Date(2026, 0, 15, 9, 0), NOW)).toBe(
      "Tomorrow at 9:00 AM",
    );
  });

  it("gives a date and counts calendar days further out", () => {
    expect(describeRun(new Date(2026, 0, 19, 9, 0), NOW)).toBe(
      "Mon, Jan 19 at 9:00 AM (in 5 days)",
    );
    expect(describeRun(new Date(2026, 1, 1, 9, 0), NOW)).toBe(
      "Sun, Feb 1 at 9:00 AM (in 18 days)",
    );
  });
});

describe("describeNextRun", () => {
  it("reads as one sentence", () => {
    expect(describeNextRun(new Date(2026, 0, 14, 15, 0), NOW)).toBe(
      "Next run today at 3:00 PM (in 3 hours)",
    );
    expect(describeNextRun(new Date(2026, 0, 15, 9, 0), NOW)).toBe(
      "Next run tomorrow at 9:00 AM",
    );
    expect(describeNextRun(new Date(2026, 0, 19, 9, 0), NOW)).toBe(
      "Next run on Mon, Jan 19 at 9:00 AM (in 5 days)",
    );
  });
});

describe("timezone", () => {
  it("names the zone the way people say it", () => {
    expect(timeZoneLabel(NOW)).toBe("Eastern Standard Time");
  });

  it("knows whether the clocks change", () => {
    expect(observesDaylightSaving(NOW)).toBe(true);
    const saved = process.env.TZ;
    process.env.TZ = "Asia/Kolkata";
    try {
      expect(observesDaylightSaving(NOW)).toBe(false);
    } finally {
      process.env.TZ = saved;
    }
  });
});
