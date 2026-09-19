"use client";
import { useCallback, useEffect, useId, useState } from "react";
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
// "nothing" is noise on a screen that has other things to show.
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
  const headingId = useId();

  const load = useCallback(() => {
    // For one workflow, ask for enough of its occurrences to fill `limit`.
    const request = workflowId ? { limit: 50, per: limit } : { limit, per: 3 };
    schedules
      .upcoming(request)
      .then((list) => {
        const mine = workflowId
          ? list.filter((u) => u.workflowId === workflowId)
          : list;
        setItems(mine.slice(0, limit));
        setUnavailable(false);
      })
      .catch((e: unknown) => {
        if (e instanceof UpcomingUnavailableError) setUnavailable(true);
        // Any other failure keeps whatever was showing.
        else setItems((prev) => prev ?? []);
      });
  }, [limit, workflowId]);

  useEffect(load, [load]);
  usePolling(load, REFRESH_MS);
  const now = useNow(!!items?.length, CLOCK_MS);

  if (unavailable || items === null) return null;
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
