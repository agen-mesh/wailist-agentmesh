"use client";
import { useMemo, useState } from "react";
import { IconSearch } from "@/components/ui";
import { Skeleton } from "@/components/ui/Skeleton";
import { useNow } from "@/hooks/useNow";
import type { Workflow } from "@/lib/types";
import {
  filterWorkflows,
  sortWorkflows,
  type Sort,
  type StatusFilter,
} from "@/lib/workflowList";
import { CreditStrip } from "./CreditStrip";
import { WorkflowFilterMenu } from "./WorkflowFilterMenu";
import { WorkflowPhoneRow } from "./WorkflowPhoneRow";

// "in 4 min" changes by the minute, so the list re-reads the clock twice a
// minute rather than every second.
const CLOCK_MS = 30_000;

// The Workflows screen on a phone: a title, the credit balance, search with
// a filter beside it, and one thin row per workflow. Creating and editing
// happen on a desktop, so none of the desktop page's actions appear here.
export function WorkflowsPhoneList({
  workflows,
  loading,
  error,
}: {
  workflows: Workflow[];
  loading: boolean;
  error: string | null;
}) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [sort, setSort] = useState<Sort>(null);

  const visible = useMemo(
    () => sortWorkflows(filterWorkflows(workflows, { query, status }), sort),
    [workflows, query, status, sort],
  );
  const now = useNow(
    workflows.some((w) => w.scheduleNextRunAt),
    CLOCK_MS,
  );

  const clearFilters = () => {
    setQuery("");
    setStatus("all");
    setSort(null);
  };

  return (
    <main className="wfp-page">
      <h1 className="wfp-title">Workflows</h1>
      <CreditStrip />
      {error && (
        <p className="wfp-error" role="alert">
          {error}
        </p>
      )}

      <div className="wfp-controls">
        <label className="wfp-search">
          <span className="wfp-search__icon" aria-hidden>
            <IconSearch size={14} />
          </span>
          <input
            type="search"
            className="wfp-search__input"
            placeholder="Search workflows"
            aria-label="Search workflows"
            enterKeyHint="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </label>
        <WorkflowFilterMenu
          status={status}
          onStatusChange={setStatus}
          sort={sort}
          onSortChange={setSort}
        />
      </div>

      {loading ? (
        <div aria-busy="true" aria-label="Loading workflows">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="wfp-skeleton">
              <Skeleton width="55%" height={16} />
              <Skeleton width="85%" height={28} />
            </div>
          ))}
        </div>
      ) : visible.length > 0 ? (
        <ul className="wfp-list">
          {visible.map((wf) => (
            <WorkflowPhoneRow key={wf.id} workflow={wf} now={now} />
          ))}
        </ul>
      ) : workflows.length === 0 ? (
        <p className="wfp-empty">
          No workflows yet. Create one in AgentMesh on a computer.
        </p>
      ) : (
        <div className="wfp-empty">
          No workflows match.
          <div>
            <button
              type="button"
              className="wfp-empty__clear"
              onClick={clearFilters}
            >
              Clear filters
            </button>
          </div>
        </div>
      )}
    </main>
  );
}
