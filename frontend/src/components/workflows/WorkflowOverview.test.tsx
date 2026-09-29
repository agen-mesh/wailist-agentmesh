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
  it("labels the balance and opens the credits page from the add button", () => {
    render(<WorkflowOverview />);
    expect(region().textContent).toContain("$12.50");
    fireEvent.click(screen.getByRole("button", { name: /add credits/i }));
    expect(state.push).toHaveBeenCalledWith("/billing");
  });

  it("does not show a zero balance before credits load", () => {
    state.credits = { balanceUSD: 0, balanceKnown: false };
    render(<WorkflowOverview />);
    expect(screen.queryByText("$0.00")).toBeNull();
    expect(region().textContent).not.toMatch(/\$/);
  });

  // The card shows a label and a figure, and nothing else. It used to carry
  // a caption explaining what credits are, under a heading that said so.
  it("renders the label and the figure and no caption", () => {
    render(<WorkflowOverview />);
    const text = region().textContent ?? "";
    expect(text).toContain("Credit balance");
    expect(text).toContain("$12.50");
    expect(
      text.replace("Credit balance", "").replace("Add credits", "").trim(),
    ).toBe("$12.50");
  });
});
