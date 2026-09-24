"use client";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { usePolling } from "@/hooks/usePolling";
import { useNow } from "@/hooks/useNow";
import { schedules, UpcomingUnavailableError } from "@/lib/api";
import type { UpcomingRun } from "@/lib/types";
import { workflowHref } from "@/lib/routes";
import { formatRunTime, formatUntil } from "@/lib/runFormat";

// Schedules move slowly; a minute between refreshes keeps the list true
// without polling like a running run does. The clock ticks faster so an
// "in 4 min" label never lags a whole minute.
const REFRESH_MS = 60_000;
const CLOCK_MS = 30_000;

function dayLabel(at: string, now: number): string {
  const d = new Date(at);
  const today = new Date(now);
  const tomorrow = new Date(now);
  tomorrow.setDate(today.getDate() + 1);
  if (d.toDateString() === today.toDateString()) return "Today";
  if (d.toDateString() === tomorrow.toDateString()) return "Tomorrow";
  return new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
  }).format(d);
}

// The runs the scheduler will start next, soonest first, grouped by day.
// One component for Activity, the desktop Workflows page and a workflow's
// own screen; `workflowId` narrows it to that workflow and drops the names.
//
// A server without the upcoming-runs route renders nothing, and so does an
// empty list when `hideWhenEmpty` is set: a section that only ever says
// "nothing" is noise on a screen that has other things to show. A failed load
// is neither, so it is said out loud with a retry.
export function UpcomingRuns({
  limit = 5,
  workflowId,
  title = "Upcoming",
  hideWhenEmpty = false,
}: {
  limit?: number;
  workflowId?: string;
  title?: string;
  hideWhenEmpty?: boolean;
}) {
  const [items, setItems] = useState<UpcomingRun[] | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  // The last load failed for a reason other than a missing route.
  const [failed, setFailed] = useState(false);
  const headingId = useId();
  // Numbers each load, so only the newest one started may land. Mount, the
  // refresh timer and coming back to the foreground can overlap, and an older
  // list arriving last would put back a schedule that has since changed.
  const seq = useRef(0);

  const load = useCallback(() => {
    const mine = ++seq.current;
    // For one workflow, ask for that schedule alone, enough occurrences to
    // fill `limit`. Filtering everyone's soonest 50 instead lost it whenever
    // enough other schedules came first.
    const request = workflowId
      ? { limit, per: limit, workflowId }
      : { limit, per: 3 };
    return schedules
      .upcoming(request)
      .then((list) => {
        if (mine !== seq.current) return;
        // Still filtered here: a server older than the workflowId parameter
        // ignores it and answers for every schedule.
        const shown = workflowId
          ? list.filter((u) => u.workflowId === workflowId)
          : list;
        setItems(shown.slice(0, limit));
        setUnavailable(false);
        setFailed(false);
      })
      .catch((e: unknown) => {
        if (mine !== seq.current) return;
        if (e instanceof UpcomingUnavailableError) setUnavailable(true);
        // Any other failure keeps whatever was showing, and says so only
        // when nothing was: an empty list would read as "no schedules".
        else setFailed(true);
      });
  }, [limit, workflowId]);

  useEffect(() => {
    void load();
  }, [load]);
  usePolling(load, REFRESH_MS);
  const now = useNow(!!items?.length, CLOCK_MS);

  if (unavailable) return null;
  if (items === null) {
    if (!failed) return null;
    return (
      <section className="upc" aria-labelledby={headingId}>
        <h2 id={headingId} className="upc__title">
          {title}
        </h2>
        <p className="upc__empty" role="alert">
          Couldn&rsquo;t load upcoming runs.{" "}
          <button type="button" className="upc__retry" onClick={load}>
            Retry
          </button>
        </p>
      </section>
    );
  }
  if (items.length === 0 && hideWhenEmpty) return null;

  const groups: { label: string; items: UpcomingRun[] }[] = [];
  for (const u of items) {
    const label = dayLabel(u.at, now);
    const last = groups.at(-1);
    if (last?.label === label) last.items.push(u);
    else groups.push({ label, items: [u] });
  }

  return (
    <section className="upc" aria-labelledby={headingId}>
      <h2 id={headingId} className="upc__title">
        {title}
      </h2>
      {items.length === 0 ? (
        <p className="upc__empty">Nothing scheduled.</p>
      ) : (
        groups.map((g) => (
          <div key={g.label}>
            <div className="upc__day">{g.label}</div>
            <ul className="upc__list">
              {g.items.map((u) => {
                const content = (
                  <>
                    <span className="upc__time">{formatRunTime(u.at)}</span>
                    <span className="upc__name">
                      {workflowId ? "" : u.workflowName}
                    </span>
                    <span className="upc__until">{formatUntil(u.at, now)}</span>
                  </>
                );
                return (
                  <li key={`${u.workflowId}@${u.at}`}>
                    {workflowId ? (
                      <div className="upc__row">{content}</div>
                    ) : (
                      <Link
                        href={workflowHref(u.workflowId)}
                        className="upc__row"
                      >
                        {content}
                      </Link>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        ))
      )}
    </section>
  );
}
