import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";

const state = vi.hoisted(() => ({
  pathname: "/signin",
  router: { push: vi.fn(), replace: vi.fn() },
  me: vi.fn(),
  boot: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  usePathname: () => state.pathname,
  useRouter: () => state.router,
}));
vi.mock("@/lib/nativeAuth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/nativeAuth")>()),
  IS_NATIVE: true,
  authReady: Promise.resolve(),
}));
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  BASE: "https://api.example.test",
  auth: { me: state.me },
}));
vi.mock("@/native", () => ({ boot: state.boot }));

import { NativeBoot } from "./NativeBoot";
import { AuthCheckError } from "@/lib/api";
import { setAuthToken } from "@/lib/nativeAuth";
import { navigateInApp, takePendingRoute } from "@/lib/nativeNav";

const target = "/workflows/app?id=wf-1";
const signIn = `/signin?next=${encodeURIComponent(target)}`;

beforeEach(() => {
  vi.clearAllMocks();
  state.pathname = "/signin";
  state.boot.mockResolvedValue(null);
  state.me.mockReset();
  setAuthToken("expired-token");
});

afterEach(() => {
  cleanup();
  setAuthToken(null);
  takePendingRoute();
});

describe("notification navigation", () => {
  it.each([401, 403])(
    "preserves the target when a stored token gets %i",
    async (status) => {
      state.me.mockRejectedValue(new AuthCheckError(status));
      render(<NativeBoot />);
      act(() => navigateInApp(target));

      await waitFor(() =>
        expect(state.router.replace).toHaveBeenCalledWith(signIn),
      );
      expect(state.router.push).not.toHaveBeenCalled();
    },
  );

  it("waits for session confirmation before opening a protected route", async () => {
    let confirm!: () => void;
    state.me.mockReturnValue(
      new Promise<void>((resolve) => {
        confirm = resolve;
      }),
    );
    render(<NativeBoot />);
    act(() => navigateInApp(target));

    expect(state.router.push).not.toHaveBeenCalled();
    await waitFor(() => expect(state.me).toHaveBeenCalledOnce());
    await act(async () => confirm());
    expect(state.router.push).toHaveBeenCalledWith(target);
  });

  it("does not follow a success for a session that signed out during the check", async () => {
    let confirm!: () => void;
    state.me.mockReturnValue(
      new Promise<void>((resolve) => {
        confirm = resolve;
      }),
    );
    render(<NativeBoot />);
    act(() => navigateInApp(target));
    await waitFor(() => expect(state.me).toHaveBeenCalledOnce());

    setAuthToken(null);
    await act(async () => confirm());
    expect(state.router.push).not.toHaveBeenCalled();
    expect(state.router.replace).toHaveBeenCalledWith(signIn);
  });

  it("keeps only the latest notification while checks settle out of order", async () => {
    let confirm!: () => void;
    state.me.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        confirm = resolve;
      }),
    );
    state.me.mockResolvedValueOnce({ id: "user" });
    render(<NativeBoot />);
    act(() => navigateInApp(target));
    await waitFor(() => expect(state.me).toHaveBeenCalledOnce());
    act(() => navigateInApp("/workflows/app?id=wf-2"));
    await waitFor(() =>
      expect(state.router.push).toHaveBeenCalledWith("/workflows/app?id=wf-2"),
    );

    await act(async () => confirm());
    expect(state.router.push).toHaveBeenCalledTimes(1);
  });

  it("checks a route held during launch when the launch redirect lands", async () => {
    state.pathname = "/";
    state.me.mockRejectedValue(new AuthCheckError(401));
    const view = render(<NativeBoot />);
    act(() => navigateInApp(target));
    expect(state.router.push).not.toHaveBeenCalled();

    state.pathname = "/signin";
    view.rerender(<NativeBoot />);
    await waitFor(() =>
      expect(state.router.replace).toHaveBeenCalledWith(signIn),
    );
    expect(state.router.push).not.toHaveBeenCalled();
  });

  it("does not redirect after unmount while a check is pending", async () => {
    let confirm!: () => void;
    state.me.mockReturnValue(
      new Promise<void>((resolve) => {
        confirm = resolve;
      }),
    );
    const view = render(<NativeBoot />);
    act(() => navigateInApp(target));
    await waitFor(() => expect(state.me).toHaveBeenCalledOnce());

    view.unmount();
    await act(async () => confirm());
    expect(state.router.push).not.toHaveBeenCalled();
    expect(state.router.replace).not.toHaveBeenCalled();
  });
});
