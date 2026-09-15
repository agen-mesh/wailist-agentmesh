import type { RunSummary } from "./types";

export interface RunDayGroup {
  // The local calendar day, "YYYY-MM-DD". Stable, so it can key a list.
  key: string;
  label: string;
  runs: RunSummary[];
}

// The device's own calendar day, not a UTC one and not "within 24 hours":
// a run at 23:50 and one at 00:10 are different days to the person holding
// the phone, and a daylight-saving change must not move a run across a line.
function localDayKey(d: Date): string {
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${d.getFullYear()}-${month}-${day}`;
}

// Groups runs by the local day they started, keeping their order (the list
// arrives newest first) and labelling each day Today, Yesterday or a date.
// `now` and `locale` are parameters so the labels can be tested.
export function groupRunsByDay(
  runs: readonly RunSummary[],
  now: Date = new Date(),
  locale?: string,
): RunDayGroup[] {
  const today = localDayKey(now);
  const yesterday = localDayKey(
    new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1),
  );
  const thisYear = new Intl.DateTimeFormat(locale, {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
  const otherYear = new Intl.DateTimeFormat(locale, {
    day: "numeric",
    month: "short",
    year: "numeric",
  });

  const groups = new Map<string, RunDayGroup>();
  for (const run of runs) {
    const started = new Date(run.startedAt);
    if (Number.isNaN(started.getTime())) continue;
    const key = localDayKey(started);
    let group = groups.get(key);
    if (!group) {
      const label =
        key === today
          ? "Today"
          : key === yesterday
            ? "Yesterday"
            : started.getFullYear() === now.getFullYear()
              ? thisYear.format(started)
              : otherYear.format(started);
      group = { key, label, runs: [] };
      groups.set(key, group);
    }
    group.runs.push(run);
  }
  return [...groups.values()];
}
