import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({
  push: vi.fn(),
  credits: { balanceUSD: 12.5, balanceKnown: true },
}));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: state.push }) }));
vi.mock("@/lib/credits/store", () => ({ useCredits: () => state.credits }));

import { CreditStrip } from "./CreditStrip";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  state.credits = { balanceUSD: 12.5, balanceKnown: true };
});

describe("CreditStrip", () => {
  it("shows the balance beside its label", () => {
    render(<CreditStrip />);
    expect(screen.getByText("Credit balance")).toBeTruthy();
    expect(screen.getByText("$12.50")).toBeTruthy();
  });

  it("shows a dash, not $0.00, until the balance is known", () => {
    state.credits = { balanceUSD: 0, balanceKnown: false };
    render(<CreditStrip />);
    expect(screen.getByText("—")).toBeTruthy();
    expect(screen.queryByText("$0.00")).toBeNull();
  });

  it("opens billing from the + button", () => {
    render(<CreditStrip />);
    fireEvent.click(screen.getByRole("button", { name: "Add credits" }));
    expect(state.push).toHaveBeenCalledWith("/billing");
  });
});
