"use client";
import { useEffect, useId, useRef, useState } from "react";
import { IconFilter } from "@/components/ui";
import { useCloseOnBack } from "@/hooks/useCloseOnBack";
import {
  isOptionActive,
  nextSort,
  type Sort,
  type SortOption,
  type StatusFilter,
} from "@/lib/workflowList";

const SHOW: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "deployed", label: "Deployed" },
  { value: "paused", label: "Paused" },
  { value: "draft", label: "Draft" },
];

const SORTS: { value: SortOption; label: string }[] = [
  { value: "alpha", label: "Alphabetical" },
  { value: "recentRun", label: "Recently run" },
  { value: "recentCreated", label: "Recently created" },
  { value: "costHigh", label: "Highest cost" },
  { value: "costLow", label: "Lowest cost" },
];

// Which way the applied sort runs, shown beside it so a second tap visibly
// does something.
function directionHint(sort: NonNullable<Sort>): string {
  const asc = sort.dir === "asc";
  switch (sort.key) {
    case "alpha":
      return asc ? "A–Z" : "Z–A";
    case "cost":
      return asc ? "low → high" : "high → low";
    default:
      return asc ? "oldest first" : "newest first";
  }
}

// The filter and sort dropdown on the phone Workflows list. A dropdown rather
// than a sheet: it stays open while options are tapped, so several can be
// tried in a row, and it gets out of the way the moment the list scrolls.
//
// A disclosure of ordinary toggle buttons, not an ARIA menu. role="menu"
// promises arrow-key navigation and managed focus; plain buttons are reached
// with Tab like everything else on the page, and aria-pressed says which
// options are on.
export function WorkflowFilterMenu({
  status,
  onStatusChange,
  sort,
  onSortChange,
}: {
  status: StatusFilter;
  onStatusChange: (status: StatusFilter) => void;
  sort: Sort;
  onSortChange: (sort: Sort) => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const showId = useId();
  const sortId = useId();

  // Android Back closes the menu instead of leaving the screen.
  useCloseOnBack(() => setOpen(false), open);

  useEffect(() => {
    if (!open) return;
    const inside = (target: EventTarget | null) =>
      target instanceof Node && !!rootRef.current?.contains(target);
    const onPointerDown = (e: PointerEvent) => {
      if (!inside(e.target)) setOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      triggerRef.current?.focus();
    };
    // Closes on the gesture as well as on the scroll it causes: a list short
    // enough to fit the screen never scrolls, so waiting for a scroll event
    // would leave the menu up while the finger drags. Scroll is caught in the
    // capture phase because the list scrolls inside PullToRefresh's own
    // container, and a scroll event does not bubble up to window.
    const onScrollOrDrag = (e: Event) => {
      if (!inside(e.target)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    window.addEventListener("scroll", onScrollOrDrag, true);
    document.addEventListener("touchmove", onScrollOrDrag, { passive: true });
    document.addEventListener("wheel", onScrollOrDrag, { passive: true });
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("scroll", onScrollOrDrag, true);
      document.removeEventListener("touchmove", onScrollOrDrag);
      document.removeEventListener("wheel", onScrollOrDrag);
    };
  }, [open]);

  const changed = status !== "all" || sort !== null;

  return (
    <div className="wfp-filter" ref={rootRef}>
      <button
        ref={triggerRef}
        type="button"
        className="wfp-filter__trigger"
        aria-label={changed ? "Filter and sort, changed" : "Filter and sort"}
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        <IconFilter size={16} />
        {changed && <span className="wfp-filter__badge" aria-hidden />}
      </button>
      {open && (
        <div
          id={panelId}
          className="wfp-menu"
          role="group"
          aria-label="Filter and sort options"
        >
          <div className="wfp-menu__group" id={showId}>
            Show
          </div>
          <div role="group" aria-labelledby={showId}>
            {SHOW.map((o) => (
              <button
                key={o.value}
                type="button"
                aria-pressed={status === o.value}
                className="wfp-menu__item"
                onClick={() => onStatusChange(o.value)}
              >
                {o.label}
                {status === o.value && (
                  <span className="wfp-menu__check" aria-hidden>
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
                      <path
                        d="M3.5 8.5 L6.5 11.5 L12.5 4.5"
                        stroke="currentColor"
                        strokeWidth="1.5"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                    </svg>
                  </span>
                )}
              </button>
            ))}
          </div>
          <div className="wfp-menu__divider" aria-hidden />
          <div className="wfp-menu__group" id={sortId}>
            Sort by
          </div>
          <div role="group" aria-labelledby={sortId}>
            {SORTS.map((o) => {
              const active = isOptionActive(sort, o.value);
              const hint = active && sort ? directionHint(sort) : null;
              return (
                <button
                  key={o.value}
                  type="button"
                  aria-pressed={active}
                  aria-label={hint ? `${o.label}, ${hint}` : o.label}
                  className="wfp-menu__item"
                  onClick={() => onSortChange(nextSort(sort, o.value))}
                >
                  {o.label}
                  {hint && <span className="wfp-menu__hint">{hint}</span>}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
