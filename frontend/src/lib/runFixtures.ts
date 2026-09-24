import type { DeadLetterRun, RunDetail, RunLogRecord } from "./api";
import { WORKFLOWS } from "./data";
import type { RunPage, RunStatus, RunSummary } from "./types";
import { fixtureNode } from "./workflowFixtures";

// Run history for mock mode (NEXT_PUBLIC_API_URL unset).
//
// Timestamps are built from the current time on every call, so the screens
// that group runs by day always have something under Today and Yesterday no
// matter when the page is opened. Workflow ids and names match WORKFLOWS in
// data.ts, and step node ids match that workflow's graph in
// workflowFixtures.ts, so a run links to a workflow the mock list really
// shows and its steps are that workflow's own nodes.
//
// A run's list row and its detail are both built from the same script below.
// The detail used to be one weather run whatever id was asked for, so every
// run sheet showed the same answer, and a failed run claimed success.

const HOUR_MS = 3_600_000;

type LogStatus = RunLogRecord["status"];

interface StepScript {
  // Node id in the workflow's graph.
  node: string;
  // How long the step took. Absent while it is still going.
  ms?: number;
  status?: Extract<LogStatus, "running" | "failed">;
  // What an x402 step settled, in USD micros.
  paid?: number;
  output?: Record<string, unknown>;
}

interface FixtureRun {
  id: string;
  workflowId: string;
  workflowName: string;
  triggeredBy: string;
  status: RunStatus;
  hoursAgo: number;
  steps: StepScript[];
  // What went wrong, for a failed run's Problems list.
  problem?: { node: string; error: string };
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
    steps: [
      {
        node: "t4",
        ms: 1400,
        paid: 12_000,
        output: {
          response: {
            order: "#48213",
            status: "Delivered",
            items: ["Trail running shoes, size 10"],
          },
        },
      },
      {
        node: "t2",
        ms: 5200,
        output: {
          classification: "Product fault · normal priority",
          summary: "The sole of the left shoe is peeling after one run.",
        },
      },
      { node: "t7", status: "running" },
    ],
  },
  {
    id: "r-1909",
    ...TRIAGE,
    triggeredBy: "webhook",
    status: "success",
    hoursAgo: 0.6,
    steps: [
      {
        node: "t4",
        ms: 1300,
        paid: 12_000,
        output: {
          response: { order: "#48177", status: "In transit", carrier: "UPS" },
        },
      },
      {
        node: "t5",
        ms: 2100,
        paid: 18_000,
        output: {
          response: {
            lastEvent: "Held at the Louisville hub (weather delay)",
            eta: "Thursday",
          },
        },
      },
      {
        node: "t2",
        ms: 6800,
        output: { classification: "Delivery delay · high priority" },
      },
      {
        node: "t7",
        ms: 7400,
        output: {
          message:
            "**Reply sent to Maya R. (ticket #20931)**\n\n" +
            "Hi Maya, your order #48177 is held at the UPS hub in Louisville " +
            "because of a weather delay. The new delivery estimate is " +
            "**Thursday**. I've added free express shipping to your next " +
            "order for the wait.\n\nCategory: delivery delay · Priority: high",
        },
      },
      { node: "t8", ms: 900, output: { status: 201 } },
    ],
  },
  {
    id: "r-1908",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 2.5,
    steps: [
      {
        node: "b2",
        ms: 1800,
        paid: 40_000,
        output: {
          response: { spxFutures: "+0.4%", ust10y: "4.12%", brent: "$81.30" },
        },
      },
      {
        node: "b3",
        ms: 2600,
        paid: 60_000,
        output: { response: { headlines: 12 } },
      },
      {
        node: "b4",
        ms: 900,
        paid: 40_000,
        output: { response: { eurusd: 1.1045, usdjpy: 155.2 } },
      },
      {
        node: "b5",
        ms: 14_200,
        output: {
          message:
            "**Pre-market brief**\n\n" +
            "- S&P 500 futures **+0.4%** after a softer CPI print (2.6% y/y).\n" +
            "- 10-year Treasury yield down 6 bp to **4.12%**.\n" +
            "- Brent **$81.30** (+1.8%) on Red Sea shipping disruption.\n" +
            "- EUR/USD **1.1045**, a three-week high.\n" +
            "- Watch: Fed minutes at 14:00 ET.",
        },
      },
      {
        node: "b7",
        ms: 1100,
        output: { sentTo: "desk@acme-capital.example" },
      },
    ],
  },
  {
    id: "r-1907",
    ...ONCHAIN,
    triggeredBy: "geofence",
    status: "failed",
    hoursAgo: 5,
    steps: [
      {
        node: "o2",
        ms: 1500,
        paid: 8_000,
        output: {
          response: {
            wallet: "0x7a3f…c91e",
            risk: "low",
            exposure: "0.2% to a flagged exchange",
          },
        },
      },
      {
        node: "o3",
        ms: 30_100,
        status: "failed",
        output: { error: "503 Service Unavailable" },
      },
    ],
    problem: {
      node: "o3",
      error:
        "Sanctions List returned 503 Service Unavailable three times in a " +
        "row, so 0x7a3f…c91e was not screened. Nothing was sent to #compliance.",
    },
  },
  {
    id: "r-1906",
    ...TRIAGE,
    triggeredBy: "manual",
    status: "stopped",
    hoursAgo: 26,
    steps: [
      {
        node: "t4",
        ms: 1200,
        paid: 12_000,
        output: {
          response: { order: "#47950", total: "$612.00", status: "Delivered" },
        },
      },
      {
        node: "t6",
        ms: 800,
        paid: 5_000,
        output: { response: { score: 0.91 } },
      },
      {
        node: "t2",
        ms: 7300,
        output: {
          classification: "Refund over $500 · needs a person",
          note: "Stopped by hand before a reply was drafted.",
        },
      },
    ],
  },
  {
    id: "r-1905",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 27,
    steps: [
      {
        node: "b2",
        ms: 1700,
        paid: 40_000,
        output: { response: { spxFutures: "-0.7%", gold: "$2,412" } },
      },
      {
        node: "b3",
        ms: 2400,
        paid: 60_000,
        output: { response: { page: 1, headlines: 20 } },
      },
      {
        node: "b3",
        ms: 2500,
        paid: 60_000,
        output: { response: { page: 2, headlines: 9 } },
      },
      {
        node: "b5",
        ms: 16_800,
        output: {
          message:
            "**Pre-market brief**\n\n" +
            "- S&P 500 futures **−0.7%**; chip stocks lower on export-rule headlines.\n" +
            "- 10-year yield **4.18%**, flat.\n" +
            "- Gold **$2,412** (+0.9%), a record close.\n" +
            "- USD/JPY **157.8**, with intervention talk from Tokyo again.\n" +
            "- Watch: jobless claims at 08:30 ET, two Fed speakers after lunch.",
        },
      },
      {
        node: "b7",
        ms: 1000,
        output: { sentTo: "desk@acme-capital.example" },
      },
    ],
  },
  {
    id: "r-1904",
    ...INVOICE,
    triggeredBy: "manual",
    status: "success",
    hoursAgo: 30,
    steps: [
      {
        node: "i2",
        ms: 9800,
        paid: 21_000,
        output: { response: { pages: 7, invoicesRead: 7 } },
      },
      { node: "i3", ms: 1400, output: { transactions: 64 } },
      {
        node: "i4",
        ms: 11_900,
        output: {
          message:
            "**Month-end close: 42 invoices checked**\n\n" +
            "- **40** matched to a bank payment.\n" +
            "- **Northwind Logistics INV-2291** ($4,380.00) was paid twice, on the 3rd and the 17th.\n" +
            "- **Globex Office Supply INV-0874** ($212.40) has no matching payment yet.\n\n" +
            "Both are in the email to finance.",
        },
      },
      {
        node: "i6",
        ms: 800,
        output: { sentTo: "finance@acme-capital.example" },
      },
    ],
  },
  {
    id: "r-1903",
    ...ONCHAIN,
    triggeredBy: "geofence",
    status: "success",
    hoursAgo: 52,
    steps: [
      {
        node: "o2",
        ms: 1400,
        paid: 8_000,
        output: { response: { wallet: "0x51c0…4be2", risk: "low" } },
      },
      {
        node: "o3",
        ms: 600,
        paid: 1_000,
        output: { response: { listed: false } },
      },
      {
        node: "o4",
        ms: 6100,
        output: {
          message:
            "**Treasury inflows: 3 transfers reviewed**\n\n" +
            "All three counterparties screened clean, with no sanctions hits " +
            "and no exposure above 1% to high-risk sources. The largest was " +
            "**48,500 USDC** from 0x51c0…4be2, a known OTC desk. Nothing " +
            "needs a compliance officer today.",
        },
      },
      { node: "o6", ms: 700, output: { channel: "#compliance" } },
    ],
  },
  {
    id: "r-1902",
    ...BRIEF,
    triggeredBy: "schedule",
    status: "success",
    hoursAgo: 75,
    steps: [
      {
        node: "b2",
        ms: 1600,
        paid: 40_000,
        output: { response: { spxFutures: "flat", brent: "-2.4%" } },
      },
      {
        node: "b3",
        ms: 2300,
        paid: 60_000,
        output: { response: { headlines: 15 } },
      },
      {
        node: "b5",
        ms: 12_900,
        output: {
          message:
            "**Pre-market brief**\n\n" +
            "- Futures flat ahead of payrolls (consensus **+185k**).\n" +
            "- Oil **−2.4%** as OPEC+ signals higher output next month.\n" +
            "- 2-year yield **4.71%**, pricing roughly two cuts this year.\n" +
            "- Watch: payrolls at 08:30 ET; unemployment expected at 4.0%.\n\n" +
            "_FX rates skipped: the currency market was closed for the holiday._",
        },
      },
      {
        node: "b7",
        ms: 1000,
        output: { sentTo: "desk@acme-capital.example" },
      },
    ],
  },
  {
    id: "r-1901",
    ...TRIAGE,
    triggeredBy: "webhook",
    status: "success",
    hoursAgo: 100,
    steps: [
      {
        node: "t4",
        ms: 1100,
        paid: 12_000,
        output: {
          response: {
            order: "#46802",
            status: "Delivered",
            items: ["Merino hoodie, size M"],
          },
        },
      },
      {
        node: "t6",
        ms: 700,
        paid: 5_000,
        output: { response: { score: 0.34 } },
      },
      {
        node: "t2",
        ms: 5900,
        output: { classification: "Exchange · normal priority" },
      },
      {
        node: "t7",
        ms: 6600,
        output: {
          message:
            "**Reply sent to Daniel K. (ticket #20544)**\n\n" +
            "Hi Daniel, happy to swap the merino hoodie on order #46802 for " +
            "a size L. I've emailed you a prepaid return label, and the new " +
            "one ships as soon as the courier scans your parcel.\n\n" +
            "Category: exchange · Priority: normal",
        },
      },
      { node: "t8", ms: 800, output: { status: 201 } },
    ],
  },
];

// What a run started from the app does, per workflow. Only the workflows the
// summary lets you run (deployed, not chat) need one.
const STARTED_RUN_STEPS: Record<string, StepScript[]> = {
  "wf-triage": [
    {
      node: "t4",
      ms: 1500,
      paid: 12_000,
      output: { response: { order: "#48240", status: "Processing" } },
    },
    {
      node: "t2",
      ms: 2500,
      output: { classification: "Address change · normal priority" },
    },
    {
      node: "t7",
      ms: 2500,
      output: {
        message:
          "**Reply sent to Priya S. (ticket #20958)**\n\n" +
          "Hi Priya, order #48240 hasn't shipped yet, so I've changed the " +
          "delivery address to the one in your message. Tracking will reach " +
          "you by email once it leaves the warehouse.\n\n" +
          "Category: address change · Priority: normal",
      },
    },
    { node: "t8", ms: 700, output: { status: 201 } },
  ],
  "wf-brief": [
    {
      node: "b2",
      ms: 1500,
      paid: 40_000,
      output: { response: { spx: "+0.2%", ust10y: "4.10%" } },
    },
    {
      node: "b3",
      ms: 2000,
      paid: 60_000,
      output: { response: { headlines: 4 } },
    },
    {
      node: "b5",
      ms: 3000,
      output: {
        message:
          "**Intraday check**\n\n" +
          "- S&P 500 **+0.2%** since the open, tech leading.\n" +
          "- 10-year yield **4.10%**.\n" +
          "- No headlines since this morning's brief that change the view.",
      },
    },
    {
      node: "b7",
      ms: 600,
      output: { sentTo: "desk@acme-capital.example" },
    },
  ],
  "wf-onchain": [
    {
      node: "o2",
      ms: 1500,
      paid: 8_000,
      output: { response: { wallet: "0x9d44…07af", risk: "none" } },
    },
    {
      node: "o3",
      ms: 800,
      paid: 1_000,
      output: { response: { listed: false } },
    },
    {
      node: "o4",
      ms: 3000,
      output: {
        message:
          "**Manual screening: 1 new transfer**\n\n" +
          "**12,000 USDC** from 0x9d44…07af screened clean, with no " +
          "sanctions hits and no exposure to high-risk sources.",
      },
    },
    { node: "o6", ms: 600, output: { channel: "#compliance" } },
  ],
};

// Runs started with workflows.run in this session, by id.
const startedRuns = new Map<
  string,
  { workflowId: string; startedAt: number }
>();

// Remembers a run started from the app, so it shows up in the lists and its
// detail runs through its steps in real time. Workflows without a script are
// ignored, and their runs keep the generic detail api.ts falls back to.
export function recordStartedRun(
  runId: string,
  workflowId: string,
  now: number = Date.now(),
): void {
  if (STARTED_RUN_STEPS[workflowId]) {
    startedRuns.set(runId, { workflowId, startedAt: now });
  }
}

const totalMs = (steps: StepScript[]) =>
  steps.reduce((sum, s) => sum + (s.ms ?? 0), 0);
const spentMicros = (steps: StepScript[]) =>
  steps.reduce(
    (sum, s) => sum + (s.status === "failed" ? 0 : (s.paid ?? 0)),
    0,
  );

// A stable, per-step fake transaction id, so no two payments share one.
function fakeTxId(seed: string): string {
  let out = "";
  let h = 0x811c9dc5;
  for (let round = 0; out.length < 64; round++) {
    for (const ch of `${seed}:${round}`) {
      h = Math.imul(h ^ ch.charCodeAt(0), 0x01000193) >>> 0;
    }
    out += h.toString(16).toUpperCase().padStart(8, "0");
  }
  return out.slice(0, 64);
}

// A started run's steps as they stand `elapsed` ms in: the ones that fit are
// done, the next is running, and the rest have not begun.
function stepsSoFar(
  steps: StepScript[],
  elapsed: number,
): { steps: StepScript[]; done: boolean } {
  const out: StepScript[] = [];
  let at = 0;
  for (const s of steps) {
    at += s.ms ?? 0;
    if (at > elapsed) {
      out.push({ node: s.node, status: "running" });
      return { steps: out, done: false };
    }
    out.push(s);
  }
  return { steps: out, done: true };
}

export interface FixtureRunDetail {
  run: RunDetail;
  logs: RunLogRecord[];
  deadLetters: DeadLetterRun[];
}

interface Built {
  summary: RunSummary;
  detail: FixtureRunDetail;
}

function build(
  run: Omit<FixtureRun, "hoursAgo">,
  startedAt: number,
  now: number,
): Built {
  const finished = run.status !== "running";
  const startIso = new Date(startedAt).toISOString();
  const finishIso = finished
    ? new Date(startedAt + totalMs(run.steps)).toISOString()
    : undefined;

  let at = startedAt;
  const logs: RunLogRecord[] = run.steps.map((s, i) => {
    at += s.ms ?? 0;
    const node = fixtureNode(run.workflowId, s.node);
    const output: Record<string, unknown> = {
      nodeName: node?.name ?? node?.label ?? s.node,
      ...s.output,
    };
    if (s.paid && s.status !== "failed") {
      const txId = fakeTxId(`${run.id}:${i}`);
      Object.assign(output, {
        txId,
        amount: (s.paid / 1e6).toFixed(3),
        settledUsdMicros: s.paid,
        explorerURL: `https://allo.info/tx/${txId}`,
      });
    }
    const log: RunLogRecord = {
      id: `${run.id}-s${i}`,
      runId: run.id,
      stepIndex: i,
      nodeId: s.node,
      nodeType: node?.type ?? "tool",
      status: s.status ?? "success",
      output,
      ts: new Date(s.status === "running" ? now : at).toISOString(),
    };
    if (s.ms !== undefined) log.durationMs = s.ms;
    return log;
  });

  const deadLetters: DeadLetterRun[] = run.problem
    ? [
        {
          id: `${run.id}-dl`,
          runId: run.id,
          nodeId: run.problem.node,
          error: run.problem.error,
          attemptCount: 3,
          createdAt: finishIso ?? startIso,
        },
      ]
    : [];

  const summary: RunSummary = {
    id: run.id,
    workflowId: run.workflowId,
    workflowName: run.workflowName,
    triggeredBy: run.triggeredBy,
    status: run.status,
    startedAt: startIso,
    spendUsdMicros: spentMicros(run.steps),
  };
  if (finishIso) summary.finishedAt = finishIso;

  const detailRun: RunDetail = {
    id: run.id,
    workflowId: run.workflowId,
    triggeredBy: run.triggeredBy,
    status: run.status,
    startedAt: startIso,
  };
  if (finishIso) detailRun.finishedAt = finishIso;

  return { summary, detail: { run: detailRun, logs, deadLetters } };
}

function buildStarted(runId: string, now: number): Built | null {
  const started = startedRuns.get(runId);
  if (!started) return null;
  const script = STARTED_RUN_STEPS[started.workflowId];
  const { steps, done } = stepsSoFar(script, now - started.startedAt);
  return build(
    {
      id: runId,
      workflowId: started.workflowId,
      workflowName:
        WORKFLOWS.find((w) => w.id === started.workflowId)?.name ??
        started.workflowId,
      triggeredBy: "manual",
      status: done ? "success" : "running",
      steps,
    },
    started.startedAt,
    now,
  );
}

function fixtureRuns(now: number): RunSummary[] {
  const started = [...startedRuns.keys()]
    .flatMap((id) => buildStarted(id, now)?.summary ?? [])
    .sort((a, b) => b.startedAt.localeCompare(a.startedAt));
  const fixed = FIXTURE_RUNS.map(
    (r) => build(r, now - r.hoursAgo * HOUR_MS, now).summary,
  );
  return [...started, ...fixed];
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

// The detail of one fixture run, or of one started from the app, built from
// the same script as its list row. Null for an id neither knows.
export function fixtureRunDetail(
  runId: string,
  now: number = Date.now(),
): FixtureRunDetail | null {
  const row = FIXTURE_RUNS.find((r) => r.id === runId);
  if (row) return build(row, now - row.hoursAgo * HOUR_MS, now).detail;
  return buildStarted(runId, now)?.detail ?? null;
}
