// A workflow's schedule in words a person reads, not the cron that runs it.
//
// The backend stores schedules as standard 5-field cron in UTC
// (minute hour day-of-month month day-of-week), and the workflow screen used
// to print that as is -- "0 7 * * 1-5", which says nothing to anyone who has
// not read a crontab. This turns the common shapes into "Every weekday at
// 12:30 PM", in the reader's own time zone, and anything else into a plain
// "On a custom schedule" rather than the raw expression.
//
// Times are converted the way lib/cronCadence.ts converts them: build the real
// UTC instant, then read it back in the target zone, so the calendar and any
// daylight-saving shift are the platform's to get right. A day list moves
// with the time when local time falls on the other side of midnight.

const CUSTOM = "On a custom schedule";
const DAY_NAMES = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];
const DAY_SHORT = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function asInt(field: string, min: number, max: number): number | null {
  if (!/^\d+$/.test(field)) return null;
  const n = Number(field);
  return n >= min && n <= max ? n : null;
}

function everyN(field: string): number | null {
  const match = /^\*\/(\d+)$/.exec(field);
  return match && Number(match[1]) > 0 ? Number(match[1]) : null;
}

function every(n: number, unit: string): string {
  return n === 1 ? `Every ${unit}` : `Every ${n} ${unit}s`;
}

// Day-of-week: numbers, ranges and lists, 0 or 7 for Sunday. Names and steps
// are rare enough to be "custom".
function parseDays(field: string): number[] | null {
  const days = new Set<number>();
  for (const part of field.split(",")) {
    const range = /^(\d)-(\d)$/.exec(part);
    const from = range ? Number(range[1]) : asInt(part, 0, 7);
    const to = range ? Number(range[2]) : from;
    if (from === null || to === null || from > to || to > 7) return null;
    for (let d = from; d <= to; d++) days.add(d % 7);
  }
  return [...days];
}

function phraseDays(days: number[]): string {
  const key = days.join(",");
  if (days.length === 7) return "Every day";
  if (key === "1,2,3,4,5") return "Every weekday";
  if (key === "0,6") return "Every weekend";
  if (days.length === 1) return `Every ${DAY_NAMES[days[0]]}`;
  const names = days.map((d) => DAY_SHORT[d]);
  return `Every ${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

function ordinal(n: number): string {
  const teen = n % 100 >= 11 && n % 100 <= 13;
  const suffix = teen ? "th" : (["th", "st", "nd", "rd"][n % 10] ?? "th");
  return `${n}${suffix}`;
}

/**
 * The schedule in plain words, in `timeZone` (the reader's own when left out).
 * `now` anchors the week and month the times are computed in, so a
 * daylight-saving change is reflected once it is in effect.
 */
export function describeSchedule(
  cron: string,
  now: Date = new Date(),
  timeZone?: string,
): string {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return CUSTOM;
  const [minute, hour, dom, month, dow] = parts;
  if (month !== "*") return CUSTOM;

  // Repeating intervals: no time of day to convert.
  if (dom === "*" && dow === "*") {
    if (minute === "*" && hour === "*") return "Every minute";
    const minutes = everyN(minute);
    if (minutes !== null && hour === "*") return every(minutes, "minute");
    if (asInt(minute, 0, 59) !== null) {
      if (hour === "*") return "Every hour";
      const hours = everyN(hour);
      if (hours !== null) return every(hours, "hour");
    }
  }

  const m = asInt(minute, 0, 59);
  const h = asInt(hour, 0, 23);
  if (m === null || h === null) return CUSTOM;

  const read = (instant: Date, options: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat("en-US", { ...options, timeZone }).format(instant);
  // Some ICU versions put a narrow no-break space before AM/PM.
  const at = (instant: Date) =>
    read(instant, { hour: "numeric", minute: "2-digit" }).replace(/\s/g, " ");
  const year = now.getUTCFullYear();
  const mon = now.getUTCMonth();
  const date = now.getUTCDate();

  // Every description is of the NEXT run, never of one already past. Today's
  // run, or this week's, can sit on the other side of a daylight-saving
  // change from the next one, and then its clock time is simply wrong: on 31
  // October in New York, 07:00 UTC has already happened at 3:00 AM, while the
  // run being described happens at 2:00 AM.
  if (dom === "*" && dow === "*") {
    const today = new Date(Date.UTC(year, mon, date, h, m));
    const next =
      today >= now ? today : new Date(Date.UTC(year, mon, date + 1, h, m));
    return `Every day at ${at(next)}`;
  }

  if (dom === "*") {
    const utcDays = parseDays(dow);
    if (!utcDays) return CUSTOM;
    // The same weekdays, week by week. Week 0 is the one `now` is in.
    const week = (offset: number) =>
      utcDays.map(
        (d) =>
          new Date(
            Date.UTC(year, mon, date - now.getUTCDay() + d + offset * 7, h, m),
          ),
      );
    // One formatter for every sample: the year's worth below would otherwise
    // build a few hundred of them.
    const weekday = new Intl.DateTimeFormat("en-US", {
      weekday: "short",
      timeZone,
    });
    const localDays = (instants: Date[]) =>
      [
        ...new Set(instants.map((i) => DAY_SHORT.indexOf(weekday.format(i)))),
      ].sort((a, b) => a - b);
    const upcoming = [...week(0), ...week(1)]
      .filter((i) => i >= now)
      .sort((a, b) => +a - +b);
    const next = upcoming[0] ?? week(1)[0];
    // The week the next run falls in is the one whose local days are named.
    const nextWeek = week(0).some((i) => +i === +next) ? 0 : 1;
    // A time-zone offset change can move a run to another local weekday.
    // Check every occurrence for more than a year because some zones have
    // short offset pauses that a seasonal sample would miss:
    // Africa/Casablanca sits at UTC+1 but drops to UTC for Ramadan, a few
    // weeks that come earlier each year.
    const named = localDays(week(nextWeek));
    const namedKey = named.join();
    for (let offset = nextWeek + 1; offset <= nextWeek + 53; offset++) {
      if (namedKey !== localDays(week(offset)).join()) return CUSTOM;
    }
    return `${phraseDays(named)} at ${at(next)}`;
  }

  // Monthly. One sampled month is not enough: Date.UTC rolls a day the month
  // lacks (the 31st in September) into the next month, and a day near
  // midnight can land on a different local day depending on the month's
  // length. So every month of the year is checked -- skipping months that do
  // not have the day, as the scheduler does -- and a single local day is
  // named only when every month agrees on it.
  //
  // The twelve months start from this one, and the time given is the next
  // run's. Daylight saving moves the local clock time across the year, and
  // the one a reader needs is the one that is coming.
  const day = asInt(dom, 1, 31);
  if (dow === "*" && day !== null) {
    const instants = Array.from(
      { length: 12 },
      (_, i) => new Date(Date.UTC(year, mon + i, day, h, m)),
    ).filter((instant) => instant.getUTCDate() === day);
    const localDays = new Set(
      instants.map((instant) => read(instant, { day: "numeric" })),
    );
    if (instants.length === 0 || localDays.size !== 1) return CUSTOM;
    const localDay = Number([...localDays][0]);
    // This month's run may already be past; the next is then a month on.
    const next = instants.find((instant) => instant >= now) ?? instants[0];
    return `Every month on the ${ordinal(localDay)} at ${at(next)}`;
  }

  return CUSTOM;
}
