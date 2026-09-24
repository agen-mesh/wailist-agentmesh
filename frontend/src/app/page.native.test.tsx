import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

const state = vi.hoisted(() => ({
  replace: vi.fn(),
  auth: { signedIn: false, loading: false, offline: false, retry: vi.fn() },
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace }),
}));
vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: true }));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => state.auth }));
vi.mock("@/components/landing/LandingPage", () => ({
  LandingPage: () => null,
}));
vi.mock("@/components/native/OfflineScreen", () => ({
  OfflineScreen: () => null,
}));

import Home from "./page";
import { navigateInApp, takePendingRoute } from "@/lib/nativeNav";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  takePendingRoute();
  state.auth.signedIn = false;
  state.auth.loading = false;
  state.auth.offline = false;
});

describe("native launch notification", () => {
  it("keeps a signed-out launch target in the sign-in URL", () => {
    navigateInApp("/workflows/app?id=wf-1");
    render(<Home />);
    expect(state.replace).toHaveBeenCalledWith(
      "/signin?next=%2Fworkflows%2Fapp%3Fid%3Dwf-1",
    );
    expect(takePendingRoute()).toBeNull();
  });

  it("opens the held workflow once the launch session is confirmed", () => {
    state.auth.signedIn = true;
    navigateInApp("/workflows/app?id=wf-1");
    render(<Home />);
    expect(state.replace).toHaveBeenCalledWith("/workflows/app?id=wf-1");
  });

  it.each(["loading", "offline"] as const)(
    "keeps the notification pending while %s",
    (key) => {
      state.auth[key] = true;
      navigateInApp("/workflows/app?id=wf-1");
      render(<Home />);
      expect(state.replace).not.toHaveBeenCalled();
      expect(takePendingRoute()?.href).toBe("/workflows/app?id=wf-1");
    },
  );
});
