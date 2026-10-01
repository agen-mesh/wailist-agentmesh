import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

// The app's launch page: where it sends the user once the session check has an
// answer, and what it shows when the check could not reach the server.
const state = vi.hoisted(() => ({
  auth: {
    signedIn: false,
    loading: false,
    offline: false,
    retry: vi.fn(),
  },
  replace: vi.fn(),
}));

vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: true }));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => state.auth }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace }),
}));
vi.mock("@/components/landing/LandingPage", () => ({
  LandingPage: () => <div>landing</div>,
}));

import Home from "./page";

beforeEach(() => {
  state.auth = {
    signedIn: false,
    loading: false,
    offline: false,
    retry: vi.fn(),
  };
  state.replace.mockReset();
});

afterEach(() => {
  cleanup();
});

describe("app launch page", () => {
  it("offers Retry instead of sending an unreachable session to sign-in", () => {
    state.auth.offline = true;
    render(<Home />);

    expect(
      screen.getByRole("heading", { name: "Can’t reach AgentMesh" }),
    ).toBeTruthy();
    expect(state.replace).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.auth.retry).toHaveBeenCalledTimes(1);
  });

  it("shows the retry in progress", () => {
    state.auth.offline = true;
    state.auth.loading = true;
    render(<Home />);

    const button = screen.getByRole("button", { name: "Retrying…" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(state.replace).not.toHaveBeenCalled();
  });

  it("opens the workflows for a signed-in session", () => {
    state.auth.signedIn = true;
    render(<Home />);
    expect(state.replace).toHaveBeenCalledWith("/workflows");
  });

  it("opens sign-in when the server says there is no session", () => {
    render(<Home />);
    expect(state.replace).toHaveBeenCalledWith("/signin");
  });
});
