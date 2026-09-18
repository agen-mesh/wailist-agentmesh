import type { RunSummary } from "@/lib/types";

// Newest first, ties by id, as the backend orders them. Rows from `fresh`
// replace rows with the same id in `old`, so a refresh updates a run that was
// already on screen without dropping the older pages below it — the
// non-destructive counterpart to a full-page replace, needed anywhere a
// refresh can land while the user has already paged further than page one.
export function mergeRuns(
  fresh: RunSummary[],
  old: RunSummary[],
): RunSummary[] {
  const byId = new Map<string, RunSummary>();
  for (const r of old) byId.set(r.id, r);
  for (const r of fresh) byId.set(r.id, r);
  return [...byId.values()].sort((a, b) =>
    a.startedAt === b.startedAt
      ? b.id.localeCompare(a.id)
      : b.startedAt.localeCompare(a.startedAt),
  );
}
