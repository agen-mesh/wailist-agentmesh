import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

// What useAuth reports when the session check fails. Only an answer from the
// server that there is no session signs the user out; a check that never got
// an answer leaves everything as it was and can be run again.
const state = vi.hoisted(() => ({ me: vi.fn() }));

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    auth: { ...actual.auth, me: state.me, signOut: vi.fn(async () => {}) },
  };
});
vi.mock("@/lib/nativeAuth", () => ({
  IS_NATIVE: false,
  authReady: Promise.resolve(),
  getAuthToken: () => null,
  setAuthToken: vi.fn(),
}));

import { AuthCheckError } from "@/lib/api";
import { useAuth } from "./useAuth";

const USER = {
  id: "u-1",
  email: "dana@example.com",
  name: "Dana",
  orgName: "Harbor Labs",
  needsOnboarding: false,
};

beforeEach(() => {
  document.cookie = "agentmesh_ui=1; Path=/";
  state.me.mockReset();
});

afterEach(() => {
  document.cookie = "agentmesh_ui=; Path=/; Max-Age=0";
});

describe("useAuth session check", () => {
  it("reports offline, and clears nothing, when the server cannot be reached", async () => {
    state.me.mockRejectedValue(new TypeError("Failed to fetch"));
    const { result } = renderHook(() => useAuth());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.offline).toBe(true);
    expect(result.current.signedIn).toBe(false);
    expect(document.cookie).toContain("agentmesh_ui=1");
  });

  it("treats a server error the same way", async () => {
    state.me.mockRejectedValue(new AuthCheckError(503));
    const { result } = renderHook(() => useAuth());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.offline).toBe(true);
  });

  it("signs out on an answer that there is no session", async () => {
    state.me.mockRejectedValue(new AuthCheckError(401));
    const { result } = renderHook(() => useAuth());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.offline).toBe(false);
    expect(result.current.signedIn).toBe(false);
    expect(document.cookie).not.toContain("agentmesh_ui=1");
  });

  it("runs the check again on retry and signs in once the server answers", async () => {
    state.me
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(USER);
    const { result } = renderHook(() => useAuth());
    await waitFor(() => expect(result.current.offline).toBe(true));

    act(() => result.current.retry());

    await waitFor(() => expect(result.current.signedIn).toBe(true));
    expect(result.current.offline).toBe(false);
    expect(result.current.user).toEqual(USER);
    expect(state.me).toHaveBeenCalledTimes(2);
  });
});
