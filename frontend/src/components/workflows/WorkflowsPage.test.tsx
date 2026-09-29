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
    expect(screen.getByRole("button", { name: /add credits/i })).toBeTruthy();
    expect(screen.queryByText(/Rows|Grid/)).toBeNull();
    expect(screen.queryByText(/your workspace/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /^Open$|^Zone/ })).toBeNull();
  });

  it("keeps the desktop page as it was", async () => {
    render(<WorkflowsPage />);
    expect(await screen.findByText("Customer Support Triage")).toBeTruthy();
    expect(screen.getByText(/Rows/)).toBeTruthy();
    // No "Open" button: the row itself is the control. The phone assertion
    // above still checks that the desktop row actions stay off a handheld,
    // via the Zone button that lives beside where Open used to be.
    expect(screen.queryByRole("button", { name: "Open" })).toBeNull();
    // ...but the row is a div with an onClick, so the name has to be a real
    // link or the Rows view has no keyboard route into a workflow at all.
    // Raised in code review when the button was removed.
    const link = screen.getByRole("link", { name: "Customer Support Triage" });
    expect(link.getAttribute("href")).toContain("/workflows/");
    // The eyebrow, which is desktop-only chrome. It replaces an assertion on
    // the "Design, deploy, and monitor agent pipelines." subtitle that used
    // to sit under the h1 -- that line restated its own heading and is gone.
    expect(screen.getByText(/your workspace/i)).toBeTruthy();
  });
});
