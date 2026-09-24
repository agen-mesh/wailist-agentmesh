import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

// The page is tested against a stubbed API. The top bar and the pull gesture
// have their own tests, so they are reduced to what this page hands them.
const state = vi.hoisted(() => ({ readOnly: false, list: vi.fn() }));

vi.mock("@/hooks/useReadOnly", () => ({ useReadOnly: () => state.readOnly }));
vi.mock("@/lib/api", () => ({ workflows: { list: state.list } }));
vi.mock("@/lib/credits/store", () => ({
  useCredits: () => ({
    balanceUSD: 7,
    balanceKnown: true,
    refreshBalance: () => Promise.resolve(),
  }),
}));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/components/runs/UpcomingRuns", () => ({
  UpcomingRuns: () => null,
}));
vi.mock("@/components/PullToRefresh", () => ({
  PullToRefresh: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

import { WorkflowsPage } from "./WorkflowsPage";

beforeEach(() => {
  state.list.mockResolvedValue([
    {
      id: "wf-1",
      name: "Customer Support Triage",
      status: "deployed",
      nodes: [],
      edges: [],
      runs: 12,
      spend: "4.20",
    },
  ]);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  state.readOnly = false;
});

describe("WorkflowsPage", () => {
  it("gives a phone the thin list, without the desktop chrome", async () => {
    state.readOnly = true;
    render(<WorkflowsPage />);
    expect(
      await screen.findByRole("link", { name: /Customer Support Triage/ }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /add credits/ })).toBeTruthy();
    expect(screen.queryByText(/Rows|Grid/)).toBeNull();
    expect(screen.queryByText(/your workspace/i)).toBeNull();
    expect(
      screen.queryByText("Design, deploy, and monitor agent pipelines."),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: /^Open$|^Zone/ })).toBeNull();
  });

  it("keeps the desktop page as it was", async () => {
    render(<WorkflowsPage />);
    expect(await screen.findByText("Customer Support Triage")).toBeTruthy();
    expect(screen.getByText(/Rows/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open" })).toBeTruthy();
    expect(
      screen.getByText("Design, deploy, and monitor agent pipelines."),
    ).toBeTruthy();
  });
});
