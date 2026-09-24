// Converts between a user-facing schedule ("every day/week/month at a local
// time") and the standard 5-field cron expression the backend's SetSchedule
// handler parses in UTC (cron.ParseStandard: minute hour day-of-month month
// day-of-week; SetSchedule computes next := sched.Next(time.Now().UTC())).
//
// Both directions build a real Date anchored on the chosen wall-clock
// values and read it back through the OTHER timezone's accessors (local
// Date methods vs. getUTC* methods) rather than doing manual offset
// arithmetic -- Date already does correct calendar math (month/year
// rollover, DST) for whatever moment that represents, so there's nothing
// to get wrong by hand.

export type Cadence = "daily" | "weekly" | "monthly";

export interface CadenceValue {
  /** Local time-of-day, "HH:MM" (24h). */
  time: string;
  cadence: Cadence;
  /** 0 (Sun)-6 (Sat), local day-of-week. Read only when cadence === "weekly". */
  dayOfWeek?: number;
  /** 1-28, local day-of-month. Read only when cadence === "monthly". */
  dayOfMonth?: number;
}

export function cadenceToCron(
  value: CadenceValue,
  now: Date = new Date(),
): string {
  const [hStr, mStr] = value.time.split(":");
  const localHour = Number(hStr);
  const localMinute = Number(mStr);

  let local = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
    localHour,
    localMinute,
    0,
    0,
  );
  if (value.cadence === "weekly" && value.dayOfWeek !== undefined) {
    local.setDate(
      local.getDate() + ((value.dayOfWeek - local.getDay() + 7) % 7),
    );
  } else if (value.cadence === "monthly" && value.dayOfMonth !== undefined) {
    local = new Date(
      now.getFullYear(),
      now.getMonth(),
      value.dayOfMonth,
      localHour,
      localMinute,
      0,
      0,
    );
  }

  const minute = local.getUTCMinutes();
  const hour = local.getUTCHours();

  if (value.cadence === "daily") {
    return `${minute} ${hour} * * *`;
  }
  if (value.cadence === "weekly") {
    return `${minute} ${hour} * * ${local.getUTCDay()}`;
  }
  // Monthly. A standard cron names a UTC day of the month, so the local day
  // has to be expressed as one: day 15 at 21:00 in New York is the 16th at
  // 02:00 UTC, every month. That only works when the UTC day exists in every
  // month (1-28) and is the same all year. Day 28 late in the evening west
  // of UTC is the 29th, which February lacks; day 1 early in the morning
  // east of UTC is the last day of the previous month, which cron cannot
  // name; and near midnight UTC, daylight saving moves the day back and
  // forth between winter and summer. Those are refused rather than saved on
  // a day that is wrong for part of the year.
  const dayOfMonth = value.dayOfMonth ?? local.getDate();
  const utcDom = monthlyUtcDay(dayOfMonth, localHour, localMinute, now);
  return `${minute} ${hour} ${utcDom} * *`;
}

/** Thrown when a local monthly time has no single UTC day-of-month. */
export class ScheduleDayError extends Error {
  constructor() {
    super(
      "That time falls on a different day in UTC for some months, so it cannot be scheduled monthly. Pick a different time or day.",
    );
    this.name = "ScheduleDayError";
  }
}

const DAY_MS = 86_400_000;

/**
 * The UTC day-of-month a local monthly time runs on, checked in winter and
 * in summer. The backend's set_schedule applies the same rule (see
 * backend/internal/engine/nodes/graphschedule.go); keep the two in step.
 */
function monthlyUtcDay(
  dayOfMonth: number,
  hour: number,
  minute: number,
  now: Date,
): number {
  const shiftIn = (month: number) => {
    const at = new Date(now.getFullYear(), month, dayOfMonth, hour, minute);
    const localDay = Date.UTC(at.getFullYear(), at.getMonth(), at.getDate());
    const utcDay = Date.UTC(
      at.getUTCFullYear(),
      at.getUTCMonth(),
      at.getUTCDate(),
    );
    return Math.round((utcDay - localDay) / DAY_MS);
  };
  const winter = shiftIn(0);
  const summer = shiftIn(6);
  const utcDom = dayOfMonth + winter;
  if (winter !== summer || utcDom < 1 || utcDom > 28) {
    throw new ScheduleDayError();
  }
  return utcDom;
}

// cronToCadence is cadenceToCron's inverse, for pre-filling the picker when
// a schedule already exists. Only recognizes the exact shapes
// cadenceToCron produces (month field always "*") -- returns null for
// anything else (a schedule set by some other tool, or malformed), so the
// caller falls back to its own defaults rather than guessing.
export function cronToCadence(
  cron: string,
  now: Date = new Date(),
): CadenceValue | null {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const [mStr, hStr, domStr, monStr, dowStr] = parts;
  const utcMinute = Number(mStr);
  const utcHour = Number(hStr);
  if (
    !Number.isInteger(utcMinute) ||
    !Number.isInteger(utcHour) ||
    monStr !== "*"
  ) {
    return null;
  }

  if (domStr === "*" && dowStr === "*") {
    const instant = new Date(
      Date.UTC(
        now.getUTCFullYear(),
        now.getUTCMonth(),
        now.getUTCDate(),
        utcHour,
        utcMinute,
      ),
    );
    return { cadence: "daily", time: toLocalTimeString(instant) };
  }
  if (domStr === "*" && dowStr !== "*") {
    const utcDow = Number(dowStr);
    if (!Number.isInteger(utcDow) || utcDow < 0 || utcDow > 6) return null;
    const base = new Date(
      Date.UTC(
        now.getUTCFullYear(),
        now.getUTCMonth(),
        now.getUTCDate(),
        utcHour,
        utcMinute,
      ),
    );
    // Nearest UTC date (today or the next 6 days) that falls on utcDow --
    // any real date on that UTC weekday converts back to the same local
    // weekday, "nearest" is only to pick a concrete instant to convert.
    base.setUTCDate(base.getUTCDate() + ((utcDow - base.getUTCDay() + 7) % 7));
    return {
      cadence: "weekly",
      time: toLocalTimeString(base),
      dayOfWeek: base.getDay(),
    };
  }
  if (domStr !== "*" && dowStr === "*") {
    const utcDom = Number(domStr);
    if (!Number.isInteger(utcDom) || utcDom < 1 || utcDom > 31) return null;
    const base = new Date(
      Date.UTC(
        now.getUTCFullYear(),
        now.getUTCMonth(),
        utcDom,
        utcHour,
        utcMinute,
      ),
    );
    // A day outside 1-28 here means this cron wasn't produced by
    // cadenceToCron (which only ever emits a day it can prove round-trips
    // to one of 1-28) -- possibly set by another tool, per this function's
    // own docstring. Clamping into range would silently misreport a real
    // stored day-29/30/31 schedule as a materially different day instead
    // of returning null as promised, letting a subsequent Save silently
    // overwrite it with the fabricated value.
    const localDom = base.getDate();
    if (localDom < 1 || localDom > 28) return null;
    return {
      cadence: "monthly",
      time: toLocalTimeString(base),
      dayOfMonth: localDom,
    };
  }
  return null;
}

function toLocalTimeString(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const WEEKDAY_NAMES = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

/** "9:00 AM" style wall-clock label for a "HH:MM" string. */
export function formatLocalTime(time: string): string {
  const [h, m] = time.split(":").map(Number);
  if (!Number.isInteger(h) || !Number.isInteger(m)) return time;
  const suffix = h < 12 ? "AM" : "PM";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12}:${String(m).padStart(2, "0")} ${suffix}`;
}

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

/** Plain-English summary of a cadence, in the user's local time. */
export function describeCadence(value: CadenceValue): string {
  const at = formatLocalTime(value.time);
  if (value.cadence === "weekly") {
    return `Every ${WEEKDAY_NAMES[value.dayOfWeek ?? 1]} at ${at}`;
  }
  if (value.cadence === "monthly") {
    return `The ${ordinal(value.dayOfMonth ?? 1)} of every month at ${at}`;
  }
  return `Every day at ${at}`;
}

/**
 * The next local moment a cadence fires after `now`. A preview for the
 * picker only -- the backend's scheduleNextRunAt stays the source of truth
 * once a schedule is saved.
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
    const dow = value.dayOfWeek ?? 1;
    const offset = (dow - now.getDay() + 7) % 7;
    const candidate = at(y, mo, d + offset);
    return candidate > now ? candidate : at(y, mo, d + offset + 7);
  }
  const today = at(y, mo, d);
  return today > now ? today : at(y, mo, d + 1);
}
