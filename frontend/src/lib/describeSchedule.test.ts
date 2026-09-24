import { describe, expect, it } from "vitest";
import { describeSchedule } from "./describeSchedule";

// The backend stores schedules as 5-field cron in UTC. People read them as
// plain words in their own time, so every case pins a time zone.
const NOW = new Date(Date.UTC(2026, 8, 22, 12, 0));
const say = (cron: string, timeZone = "UTC") =>
  describeSchedule(cron, NOW, timeZone);

describe("describeSchedule", () => {
  it("says a daily time", () => {
    expect(say("0 9 * * *")).toBe("Every day at 9:00 AM");
    expect(say("30 14 * * *")).toBe("Every day at 2:30 PM");
  });

  it("converts the time from UTC to the reader's zone", () => {
    // 07:00 UTC is 12:30 in India.
    expect(say("0 7 * * 1-5", "Asia/Kolkata")).toBe(
      "Every weekday at 12:30 PM",
    );
  });

  it("names weekdays, weekends, one day and short lists", () => {
    expect(say("0 7 * * 1-5")).toBe("Every weekday at 7:00 AM");
    expect(say("0 10 * * 0,6")).toBe("Every weekend at 10:00 AM");
    expect(say("0 9 * * 1")).toBe("Every Monday at 9:00 AM");
    expect(say("0 9 * * 1,3,5")).toBe("Every Mon, Wed and Fri at 9:00 AM");
    expect(say("0 9 * * 7")).toBe("Every Sunday at 9:00 AM");
  });

  it("moves the day when the local time crosses midnight", () => {
    // Monday 02:00 UTC is still Sunday evening in Los Angeles.
    expect(say("0 2 * * 1", "America/Los_Angeles")).toBe(
      "Every Sunday at 7:00 PM",
    );
    // Monday-Friday 20:00 UTC is Tuesday-Saturday morning in India.
    expect(say("0 20 * * 1-5", "Asia/Kolkata")).toBe(
      "Every Tue, Wed, Thu, Fri and Sat at 1:30 AM",
    );
  });

  // Daylight saving ends in the US on 1 November 2026. A schedule described
  // on 31 October must describe the run that is coming, not the one that has
  // already happened on the old offset.
  describe("across a daylight-saving change", () => {
    const eve = new Date(Date.UTC(2026, 9, 31, 12, 0));

    it("gives a daily schedule the next run's time", () => {
      // Today's 07:00 UTC was 3:00 AM EDT and is past; the next is 2:00 AM EST.
      expect(describeSchedule("0 7 * * *", eve, "America/New_York")).toBe(
        "Every day at 2:00 AM",
      );
    });

    it("gives a weekly schedule the next run's time", () => {
      // This week's Monday has passed. The next is 2 November, by which time
      // New York is on standard time: 07:30 UTC is 2:30 AM, still a Monday.
      expect(describeSchedule("30 7 * * 1", eve, "America/New_York")).toBe(
        "Every Monday at 2:30 AM",
      );
    });

    it("says custom when the local day itself depends on the season", () => {
      // 07:30 UTC is Sunday 11:30 PM in Los Angeles on standard time and
      // Monday 12:30 AM on daylight time, so no weekday is true all year.
      expect(describeSchedule("30 7 * * 1", eve, "America/Los_Angeles")).toBe(
        "On a custom schedule",
      );
    });

    it("checks short offset pauses outside a seasonal sample", () => {
      const october = new Date(Date.UTC(2026, 9, 31, 12, 0));
      // Casablanca pauses UTC+1 around Ramadan. The Sunday UTC run is Monday
      // after midnight for most of the year, but Sunday before midnight then.
      expect(
        describeSchedule("30 23 * * 0", october, "Africa/Casablanca"),
      ).toBe("On a custom schedule");
    });
  });

  it("says a monthly day", () => {
    expect(say("0 9 1 * *")).toBe("Every month on the 1st at 9:00 AM");
    expect(say("0 9 22 * *")).toBe("Every month on the 22nd at 9:00 AM");
  });

  // NOW is in September, which has no 31st. Sampling only this month rolled
  // the day into October 1st and named the wrong day.
  it("keeps a late day that the current month does not have", () => {
    expect(say("0 9 31 * *")).toBe("Every month on the 31st at 9:00 AM");
    expect(say("0 9 29 * *")).toBe("Every month on the 29th at 9:00 AM");
  });

  // 09:00 UTC is 5:00 AM in New York while daylight saving lasts and 4:00 AM
  // after it. The time used to be read from January, so September said 4:00.
  // It is the next run that counts, so the answer follows the season.
  it("gives the time of the next run in a daylight-saving zone", () => {
    expect(say("0 9 22 * *", "America/New_York")).toBe(
      "Every month on the 22nd at 5:00 AM",
    );
    const december = new Date(Date.UTC(2026, 11, 1, 12, 0));
    expect(describeSchedule("0 9 22 * *", december, "America/New_York")).toBe(
      "Every month on the 22nd at 4:00 AM",
    );
  });

  // 02:00 UTC on the 1st is the evening before in Los Angeles -- the 31st,
  // 30th or 28th depending on the month -- so no single day is true.
  it("says custom when the local day changes from month to month", () => {
    expect(say("0 2 1 * *", "America/Los_Angeles")).toBe(
      "On a custom schedule",
    );
  });

  it("says repeating intervals", () => {
    expect(say("0 */6 * * *")).toBe("Every 6 hours");
    expect(say("0 * * * *")).toBe("Every hour");
    expect(say("*/15 * * * *")).toBe("Every 15 minutes");
    expect(say("* * * * *")).toBe("Every minute");
  });

  it("never shows the raw expression for a shape it does not know", () => {
    for (const cron of ["0 9 * 1 *", "0 9 1 * 1", "5 4 * * sun", "nope", ""]) {
      expect(say(cron)).toBe("On a custom schedule");
    }
  });
});
