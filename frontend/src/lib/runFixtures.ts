import type { RunPage, RunStatus, RunSummary } from "./types";

// Run history for mock mode (NEXT_PUBLIC_API_URL unset).
//
// Timestamps are built from the current time on every call, so the screens
// that group runs by day always have something under Today and Yesterday no
// matter when the page is opened. Workflow ids and names match WORKFLOWS in
// data.ts, so a run links to a workflow the mock list really shows.

const HOUR_MS = 3_600_000;
const MINUTE_MS = 60_000;

interface FixtureRun {
  id: string;
  workflowId: string;
  workflowName: string;
  triggeredBy: string;
  status: RunStatus;
  hoursAgo: number;
  // How long a finished run took. Ignored while running.
  durationMin: number;
  spendUsdMicros: number;
}

const TRIAGE = {
  workflowId: "wf-triage",
  workflowName: "Customer Support Triage",
};
const BRIEF = { workflowId: "wf-brief", workflowName: "Daily Market Brief" };
const INVOICE = {
  workflowId: "wf-invoice",
  workflowName: "Invoice Reconciliation",
};
const ONCHAIN = {
  workflowId: "wf-onchain",
  workflowName: "On-chain Compliance Watch",
};

// Newest first, as the backend returns them.
const FIXTURE_RUNS: readonly FixtureRun[] = [
  {
    id: "r-1910",
    ...TRIAGE,
    triggeredBy: "manual",
    status: "running",
    hoursAgo: 0.02,
    durationMin: 0,
    spendUsdMicros: 12_000,
  },
  {
    id: "r-1909",
    ...TRIAGE,
    triggeredBy: "webhook",
    status: "success",
    hoursAgo: 0.6,
    durationMin: 1.5,
    spendUsdMicros: 65_000,
  },
  {
    id: "r-1908",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 2.5,
    durationMin: 4,
    spendUsdMicros: 142_000,
  },
  {
    id: "r-1907",
    ...ONCHAIN,
    triggeredBy: "geofence",
    status: "failed",
    hoursAgo: 5,
    durationMin: 0.8,
    spendUsdMicros: 8_000,
  },
  {
    id: "r-1906",
    ...TRIAGE,
    triggeredBy: "manual",
    status: "stopped",
    hoursAgo: 26,
    durationMin: 2,
    spendUsdMicros: 30_000,
  },
  {
    id: "r-1905",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 27,
    durationMin: 3.5,
    spendUsdMicros: 139_000,
  },
  {
    id: "r-1904",
    ...INVOICE,
    triggeredBy: "manual",
    status: "success",
    hoursAgo: 30,
    durationMin: 6,
    spendUsdMicros: 21_000,
  },
  {
    id: "r-1903",
    ...ONCHAIN,
    triggeredBy: "geofence",
    status: "success",
    hoursAgo: 52,
    durationMin: 1,
    spendUsdMicros: 9_000,
  },
  {
    id: "r-1902",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 75,
    durationMin: 3.8,
    spendUsdMicros: 141_000,
  },
  {
    id: "r-1901",
    ...TRIAGE,
    triggeredBy: "webhook",
    status: "success",
    hoursAgo: 100,
    durationMin: 1.2,
    spendUsdMicros: 64_000,
  },
];

function fixtureRuns(now: number): RunSummary[] {
  return FIXTURE_RUNS.map((r) => {
    const started = now - r.hoursAgo * HOUR_MS;
    const summary: RunSummary = {
      id: r.id,
      workflowId: r.workflowId,
      workflowName: r.workflowName,
      triggeredBy: r.triggeredBy,
      status: r.status,
      startedAt: new Date(started).toISOString(),
      spendUsdMicros: r.spendUsdMicros,
    };
    if (r.status !== "running") {
      summary.finishedAt = new Date(
        started + r.durationMin * MINUTE_MS,
      ).toISOString();
    }
    return summary;
  });
}

// One page of fixture runs, optionally for a single workflow.
//
// A mock cursor is simply the index of the next row. Callers treat every
// cursor as opaque, so they cannot tell it apart from the backend's.
export function fixtureRunPage(
  options: { workflowId?: string; cursor?: string | null; limit?: number } = {},
  now: number = Date.now(),
): RunPage {
  const limit = Math.min(Math.max(Math.trunc(options.limit ?? 20), 1), 50);
  const rows = fixtureRuns(now).filter(
    (r) => !options.workflowId || r.workflowId === options.workflowId,
  );
  const parsed = Number(options.cursor ?? 0);
  const offset = Number.isInteger(parsed) && parsed > 0 ? parsed : 0;
  const end = offset + limit;
  return {
    runs: rows.slice(offset, end),
    nextCursor: end < rows.length ? String(end) : null,
  };
}
