import { describe, expect, it } from "vitest";
import { AuthCheckError, isConnectionFailure } from "./api";

describe("isConnectionFailure", () => {
  it("counts a request that never reached the server", () => {
    expect(isConnectionFailure(new TypeError("Failed to fetch"))).toBe(true);
  });

  it("counts a server that failed to answer", () => {
    expect(isConnectionFailure(new AuthCheckError(502))).toBe(true);
    expect(isConnectionFailure(new AuthCheckError(503))).toBe(true);
  });

  it("does not count being signed out", () => {
    expect(isConnectionFailure(new AuthCheckError(401))).toBe(false);
    expect(isConnectionFailure(new AuthCheckError(403))).toBe(false);
  });

  it("does not count anything else", () => {
    expect(isConnectionFailure(new Error("unexpected"))).toBe(false);
    expect(isConnectionFailure("offline")).toBe(false);
  });
});
