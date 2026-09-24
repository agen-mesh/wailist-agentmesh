import { cronToCadence, type CadenceValue } from "./cronCadence";

// Everything a person reads about a workflow schedule is written here, in
// plain English and in their own timezone. The cron expression the backend
// stores is a wire format (see cronCadence.ts): it is never shown in the UI.

const WEEKDAYS = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

/** "09:00" -> "9:00 AM", "13:30" -> "1:30 PM". */
export function formatLocalTime(time: string): string {
  const [h, m] = time.split(":").map(Number);
  if (!Number.isInteger(h) || !Number.isInteger(m)) return time;
  const period = h < 12 ? "AM" : "PM";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12}:${String(m).padStart(2, "0")} ${period}`;
}

/** 1 -> "1st", 22 -> "22nd", 13 -> "13th". */
export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

/** "Every day at 9:00 AM", "Every Monday at 9:00 AM", ... */
export function describeCadence(value: CadenceValue): string {
  const at = formatLocalTime(value.time);
  if (value.cadence === "weekly") {
    return `Every ${WEEKDAYS[value.dayOfWeek ?? 1]} at ${at}`;
  }
  if (value.cadence === "monthly") {
    return `On the ${ordinal(value.dayOfMonth ?? 1)} of every month at ${at}`;
  }
  return `Every day at ${at}`;
}

/**
 * A stored schedule in plain English, or null when it is not one of the
 * daily/weekly/monthly shapes this app writes (only possible if something
 * other than the app set it) -- callers say "a custom schedule" instead.
 */
export function describeCron(
  cron: string,
  now: Date = new Date(),
): string | null {
  const value = cronToCadence(cron, now);
  return value ? describeCadence(value) : null;
}

/**
 * The next local moment a cadence fires after `now`. A preview while the
 * user is still choosing: once a schedule is saved, the backend's
 * scheduleNextRunAt is the source of truth.
 */
export function nextLocalRun(
  value: CadenceValue,
  now: Date = new Date(),
): Date {
  const [h, m] = value.time.split(":").map(Number);
  const at = (y: number, mo: number, d: number) =>
    new Date(y, mo, d, h, m, 0, 0);
  const y = now.getFullYear();
  const mo = now.getMonth();
  const d = now.getDate();

  if (value.cadence === "monthly") {
    const dom = value.dayOfMonth ?? 1;
    const thisMonth = at(y, mo, dom);
    return thisMonth > now ? thisMonth : at(y, mo + 1, dom);
  }
  if (value.cadence === "weekly") {
    const offset = ((value.dayOfWeek ?? 1) - now.getDay() + 7) % 7;
    const candidate = at(y, mo, d + offset);
    return candidate > now ? candidate : at(y, mo, d + offset + 7);
  }
  const today = at(y, mo, d);
  return today > now ? today : at(y, mo, d + 1);
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/**
 * When a run happens, the way a person would say it: "Today at 3:00 PM",
 * "Tomorrow at 9:00 AM", "Mon, Sep 28 at 9:00 AM" -- with "in 4 days" or
 * "in 2 hours" added where the day alone doesn't say how soon.
 */
export function describeRun(run: Date, now: Date = new Date()): string {
  const time = formatLocalTime(
    `${String(run.getHours()).padStart(2, "0")}:${String(run.getMinutes()).padStart(2, "0")}`,
  );
  const tomorrow = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() + 1,
  );
  if (sameDay(run, now)) return `Today at ${time} (${relative(run, now)})`;
  if (sameDay(run, tomorrow)) return `Tomorrow at ${time}`;
  const day = run.toLocaleDateString("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
  return `${day} at ${time} (${relative(run, now)})`;
}

/** "Next run tomorrow at 9:00 AM", "Next run on Mon, Sep 28 at 9:00 AM (in 4 days)". */
export function describeNextRun(run: Date, now: Date = new Date()): string {
  const when = describeRun(run, now);
  return /^(Today|Tomorrow) /.test(when)
    ? `Next run ${when[0].toLowerCase()}${when.slice(1)}`
    : `Next run on ${when}`;
}

function relative(run: Date, now: Date): string {
  const minutes = Math.max(
    1,
    Math.round((run.getTime() - now.getTime()) / 60_000),
  );
  if (minutes < 60) return `in ${minutes} minute${minutes === 1 ? "" : "s"}`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `in ${hours} hour${hours === 1 ? "" : "s"}`;
  // Calendar days, not 24-hour blocks: Monday 9 AM seen from Thursday 8 PM
  // is "in 4 days", which is how the user counts it.
  const days = Math.round(
    (new Date(run.getFullYear(), run.getMonth(), run.getDate()).getTime() -
      new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()) /
      86_400_000,
  );
  return `in ${days} day${days === 1 ? "" : "s"}`;
}

/** The user's timezone by name, e.g. "India Standard Time". */
export function timeZoneLabel(now: Date = new Date()): string {
  try {
    const name = new Intl.DateTimeFormat("en-US", { timeZoneName: "long" })
      .formatToParts(now)
      .find((p) => p.type === "timeZoneName")?.value;
    return name || Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "your local time";
  }
}

/**
 * Whether the user's clocks change during the year. Schedules are kept in UTC
 * on the server, so only these users can see a run move by an hour.
 */
export function observesDaylightSaving(now: Date = new Date()): boolean {
  const jan = new Date(now.getFullYear(), 0, 1).getTimezoneOffset();
  const jul = new Date(now.getFullYear(), 6, 1).getTimezoneOffset();
  return jan !== jul;
}
