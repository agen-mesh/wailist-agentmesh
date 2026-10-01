import type { Workflow } from "./types";

// Which workflows a list shows. Shared by the desktop table and the phone
// list so the two can never disagree about what a filter means.

export type StatusFilter = "all" | "active" | "deployed" | "paused" | "draft";

// "Active" is the desktop tab's word for a running workflow, but the backend
// stores that state as "deployed" (models.WorkflowStatusDeployed) and never
// writes "active". Matching the literal left the tab permanently empty.
function matchesStatus(wf: Workflow, status: StatusFilter): boolean {
  if (status === "all") return true;
  if (status === "active" || status === "deployed") {
    return wf.status === "deployed" || wf.status === "active";
  }
  return wf.status === status;
}

// How a list is ordered. `null` keeps the server's order, most recently
// updated first.
export type SortKey = "alpha" | "recentRun" | "recentCreated" | "cost";
export type SortDir = "asc" | "desc";
export type Sort = { key: SortKey; dir: SortDir } | null;

// The choices a sort menu offers. Highest and lowest cost are one key seen
// from either end, so choosing the active one again flips to the other.
export type SortOption =
  "alpha" | "recentRun" | "recentCreated" | "costHigh" | "costLow";

const OPTION_SORT: Record<SortOption, { key: SortKey; dir: SortDir }> = {
  alpha: { key: "alpha", dir: "asc" },
  recentRun: { key: "recentRun", dir: "desc" },
  recentCreated: { key: "recentCreated", dir: "desc" },
  costHigh: { key: "cost", dir: "desc" },
  costLow: { key: "cost", dir: "asc" },
};

export function isOptionActive(sort: Sort, option: SortOption): boolean {
  if (!sort) return false;
  const want = OPTION_SORT[option];
  if (sort.key !== want.key) return false;
  // Both cost options share a key, so direction says which one is showing.
  return want.key !== "cost" || sort.dir === want.dir;
}

// Choosing an option applies it in its natural direction; choosing the one
// already applied reverses it.
export function nextSort(sort: Sort, option: SortOption): Sort {
  const want = OPTION_SORT[option];
  if (sort && sort.key === want.key && isOptionActive(sort, option)) {
    return { key: sort.key, dir: sort.dir === "asc" ? "desc" : "asc" };
  }
  return want;
}

function sortValue(wf: Workflow, key: SortKey): string | number | null {
  switch (key) {
    case "alpha":
      return wf.name ?? "";
    case "recentRun":
      return wf.lastRunAt ? Date.parse(wf.lastRunAt) || null : null;
    case "recentCreated":
      return wf.createdAt ? Date.parse(wf.createdAt) || null : null;
    case "cost":
      // The list sends spend as a dollar string and leaves it out at zero.
      // An unavailable aggregation has no value at all, so it sorts with the
      // other blanks rather than joining the cheapest workflows at $0.
      if (wf.statsUnavailable) return null;
      return Number.parseFloat(wf.spend ?? "0") || 0;
  }
}

const collator = new Intl.Collator(undefined, {
  sensitivity: "base",
  numeric: true,
});

// Workflows without a value for the key (never run, no creation time) sort
// last in either direction, so flipping the order never floods the top of
// the list with blanks.
export function sortWorkflows(list: Workflow[], sort: Sort): Workflow[] {
  if (!sort) return list;
  const sign = sort.dir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    const va = sortValue(a, sort.key);
    const vb = sortValue(b, sort.key);
    if (va === null || vb === null) {
      return va === vb ? 0 : va === null ? 1 : -1;
    }
    const cmp =
      typeof va === "string" && typeof vb === "string"
        ? collator.compare(va, vb)
        : (va as number) - (vb as number);
    return sign * cmp;
  });
}

export function filterWorkflows(
  list: Workflow[],
  { query, status }: { query: string; status: StatusFilter },
): Workflow[] {
  const q = query.trim().toLowerCase();
  return list.filter(
    (wf) =>
      matchesStatus(wf, status) &&
      (!q ||
        wf.name?.toLowerCase().includes(q) ||
        (wf.tags?.join(" ").toLowerCase().includes(q) ?? false)),
  );
}
