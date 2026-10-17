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

const region = () => screen.getByRole("region", { name: "Credit balance" });

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  state.credits = { balanceUSD: 12.5, balanceKnown: true };
});

describe("WorkflowOverview", () => {
  it("labels the balance and opens the credits page from the compact plus button", () => {
    render(<WorkflowOverview />);
    expect(region().textContent).toContain("$12.50");
    const add = screen.getByRole("button", { name: "Add credits" });
    expect(add.textContent).toBe("");
    expect(add.getAttribute("title")).toBe("Add credits");
    fireEvent.click(add);
    expect(state.push).toHaveBeenCalledWith("/billing");
  });

  it("does not show a zero balance before credits load", () => {
    state.credits = { balanceUSD: 0, balanceKnown: false };
    render(<WorkflowOverview />);
    expect(screen.queryByText("$0.00")).toBeNull();
    expect(region().textContent).not.toMatch(/\$/);
    expect(screen.getByRole("status").textContent).toBe("Loading balance...");
  });

  it("replaces the loading status with the original credit helper when balance arrives", () => {
    state.credits = { balanceUSD: 0, balanceKnown: false };
    const { rerender } = render(<WorkflowOverview />);
    state.credits = { balanceUSD: 12.5, balanceKnown: true };
    rerender(<WorkflowOverview />);
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByText("$12.50")).toBeTruthy();
    expect(screen.getByText("Spent as your agents call paid tools and models.")).toBeTruthy();
  });
});
