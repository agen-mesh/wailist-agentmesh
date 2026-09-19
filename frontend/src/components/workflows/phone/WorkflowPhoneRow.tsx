"use client";
import Link from "next/link";
import type { Workflow } from "@/lib/types";
import { workflowHref } from "@/lib/routes";
import { formatSpend, formatUntil } from "@/lib/runFormat";

// Status as a coloured dot and a word. Colour lives only in the dot.
const STATUS: Record<string, { label: string; color: string }> = {
  deployed: { label: "Deployed", color: "var(--accent)" },
  active: { label: "Deployed", color: "var(--accent)" },
  paused: { label: "Paused", color: "var(--warm)" },
  error: { label: "Error", color: "var(--danger)" },
  draft: { label: "Draft", color: "var(--fg-dim)" },
};

const count = new Intl.NumberFormat();

// The list sends spend as a dollar string ("4.218") and omits it at zero;
// shown through the same formatter as every run's spend.
function spent(spend: string | undefined): string {
  const dollars = Number.parseFloat(spend ?? "");
  return formatSpend(Number.isFinite(dollars) ? Math.round(dollars * 1e6) : 0);
}

// One workflow on the phone list. The whole row opens the workflow; there
// is nothing else on it to tap. Runs and Spent cover the last 30 days, the
// window the list endpoint counts.
export function WorkflowPhoneRow({
  workflow: wf,
  now,
}: {
  workflow: Workflow;
  now: number;
}) {
  const status = STATUS[wf.status ?? "draft"] ?? STATUS.draft;
  return (
    <li>
      <Link href={workflowHref(wf.id)} className="wfp-row">
        <div className="wfp-row__top">
          <span className="wfp-row__name">{wf.name}</span>
          <span className="wfp-row__status">
            <span
              className="wfp-row__dot"
              style={{ background: status.color }}
              aria-hidden
            />
            {status.label}
          </span>
        </div>
        <dl className="wfp-row__facts">
          <div>
            <dt>Spent</dt>
            <dd>{spent(wf.spend)}</dd>
          </div>
          <div>
            <dt>Runs</dt>
            <dd>{count.format(wf.runs ?? 0)}</dd>
          </div>
          <div>
            <dt>Upcoming run</dt>
            <dd>{formatUntil(wf.scheduleNextRunAt, now)}</dd>
          </div>
        </dl>
      </Link>
    </li>
  );
}
