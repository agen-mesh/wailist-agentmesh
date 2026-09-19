import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import type { Workflow } from "@/lib/types";
import { WorkflowPhoneRow } from "./WorkflowPhoneRow";

const NOW = Date.parse("2026-09-19T10:00:00.000Z");

function renderRow(overrides: Partial<Workflow> = {}) {
  const wf: Workflow = {
    id: "wf-triage",
    name: "Customer Support Triage",
    status: "deployed",
    nodes: [],
    edges: [],
    runs: 1842,
    spend: "4.218",
    tags: ["support", "production"],
    updatedAt: "2026-09-19T09:58:00.000Z",
    scheduleNextRunAt: new Date(NOW + 12 * 60_000).toISOString(),
    ...overrides,
  };
  render(
    <ul>
      <WorkflowPhoneRow workflow={wf} now={NOW} />
    </ul>,
  );
  return screen.getByRole("link");
}

const fact = (row: HTMLElement, label: string) =>
  within(row).getByText(label).nextElementSibling?.textContent;

afterEach(cleanup);

describe("WorkflowPhoneRow", () => {
  it("is one link to the workflow, with nothing else to tap", () => {
    const row = renderRow();
    expect(row.getAttribute("href")).toContain("wf-triage");
    expect(within(row).queryAllByRole("button")).toHaveLength(0);
    expect(screen.getAllByRole("link")).toHaveLength(1);
  });

  it("shows the name, status, Spent, Runs and Upcoming run", () => {
    const row = renderRow();
    expect(within(row).getByText("Customer Support Triage")).toBeTruthy();
    expect(within(row).getByText("Deployed")).toBeTruthy();
    expect(fact(row, "Spent")).toBe("$4.22");
    expect(fact(row, "Runs")).toBe((1842).toLocaleString());
    expect(fact(row, "Upcoming run")).toBe("in 12 min");
  });

  it("leaves out the logo, tags, updated time and Open or Zone", () => {
    const row = renderRow();
    expect(row.querySelector("svg")).toBeNull();
    // The tags are "support" and "production"; the title also says Support.
    expect(within(row).queryByText(/production|^#?support$/i)).toBeNull();
    expect(within(row).queryByText(/Open|Zone|Updated|ago/)).toBeNull();
  });

  it("reads a workflow that never ran and has no schedule plainly", () => {
    const row = renderRow({
      status: "draft",
      runs: undefined,
      spend: undefined,
      scheduleNextRunAt: undefined,
    });
    expect(within(row).getByText("Draft")).toBeTruthy();
    expect(fact(row, "Spent")).toBe("$0");
    expect(fact(row, "Runs")).toBe("0");
    expect(fact(row, "Upcoming run")).toBe("—");
  });
});
