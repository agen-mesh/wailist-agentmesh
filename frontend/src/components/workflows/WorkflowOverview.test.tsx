import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({
  push: vi.fn(),
  credits: { balanceUSD: 12.5, balanceKnown: true },
}));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: state.push }) }));
vi.mock("@/lib/credits/store", () => ({ useCredits: () => state.credits }));
vi.mock("@/components/runs/UpcomingRuns", () => ({ UpcomingRuns: () => null }));

import { WorkflowOverview } from "./WorkflowOverview";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  state.credits = { balanceUSD: 12.5, balanceKnown: true };
});

describe("WorkflowOverview", () => {
  it("labels the balance and opens the existing credits page from the plus button", () => {
    render(<WorkflowOverview />);
    expect(screen.getByRole("region", { name: "Credit balance" }).textContent).toContain("$12.50");
    fireEvent.click(screen.getByRole("button", { name: "Add credits" }));
    expect(state.push).toHaveBeenCalledWith("/billing");
  });

  it("does not show a zero balance before credits load", () => {
    state.credits = { balanceUSD: 0, balanceKnown: false };
    render(<WorkflowOverview />);
    expect(screen.getByText("Loading balance…")).toBeTruthy();
    expect(screen.queryByText("$0.00")).toBeNull();
  });
});
