import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

// On the phone the session is a bearer token the shell keeps on disk. When the
// server answers that the token is no good, it has to go -- from memory and
// from the device -- or NativeBoot still counts it as a session and a tapped
// notification opens a protected screen without signing in.
const state = vi.hoisted(() => ({
  me: vi.fn(),
  token: "tok_old" as string | null,
  setAuthToken: vi.fn(),
  onSessionRejected: vi.fn<(token: string) => Promise<void>>(async () => {}),
}));

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return { ...actual, auth: { ...actual.auth, me: state.me } };
});
vi.mock("@/lib/nativeAuth", () => ({
  IS_NATIVE: true,
  authReady: Promise.resolve(),
  getAuthToken: () => state.token,
  setAuthToken: state.setAuthToken,
}));
vi.mock("@/native", () => ({
  shell: { onSessionRejected: state.onSessionRejected },
}));

import { AuthCheckError } from "@/lib/api";
import { useAuth } from "./useAuth";

afterEach(() => {
  vi.clearAllMocks();
  state.token = "tok_old";
});

describe("useAuth on the phone", () => {
  it.each([401, 403])(
    "drops a token the server rejects with %i, in memory and on the device",
    async (status) => {
      state.me.mockRejectedValue(new AuthCheckError(status));
      const { result } = renderHook(() => useAuth());

      await waitFor(() => expect(result.current.loading).toBe(false));
      expect(result.current.signedIn).toBe(false);
      expect(state.setAuthToken).toHaveBeenCalledWith(null);
      await waitFor(() =>
        expect(state.onSessionRejected).toHaveBeenCalledWith("tok_old"),
      );
    },
  );

  // A server failure says nothing about the token, so it stays.
  it("keeps the token when the server fails", async () => {
    state.me.mockRejectedValue(new AuthCheckError(503));
    const { result } = renderHook(() => useAuth());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.offline).toBe(true);
    expect(state.setAuthToken).not.toHaveBeenCalled();
    expect(state.onSessionRejected).not.toHaveBeenCalled();
  });

  // AuthPage runs its own check with no token at all. Its 401 is the answer
  // to having no session, not a verdict on one, and must not clear anything:
  // it used to, and could land after a sign-in had saved a token.
  it("clears nothing when the check was made without a token", async () => {
    state.token = null;
    state.me.mockRejectedValue(new AuthCheckError(401));
    const { result } = renderHook(() => useAuth());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(state.setAuthToken).not.toHaveBeenCalled();
    expect(state.onSessionRejected).not.toHaveBeenCalled();
  });

  // The rejection names the token it was about. A sign-in that replaced it
  // while the check was in flight keeps its new token in memory; the shell
  // is told which token to clear and leaves a newer one alone.
  it("does not drop a token saved after the check went out", async () => {
    let answer!: (err: unknown) => void;
    state.me.mockReturnValue(
      new Promise((_, reject) => {
        answer = reject;
      }),
    );
    const { result } = renderHook(() => useAuth());
    await waitFor(() => expect(state.me).toHaveBeenCalled());

    state.token = "tok_new";
    answer(new AuthCheckError(401));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(state.setAuthToken).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(state.onSessionRejected).toHaveBeenCalledWith("tok_old"),
    );
  });
});
