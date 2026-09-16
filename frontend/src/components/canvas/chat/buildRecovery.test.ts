import { describe, it, expect } from "vitest";
import { recoverFinishedBuild } from "./buildProgress";

describe("recoverFinishedBuild", () => {
  // The case this exists for: the build ran past the proxy window, its POST
  // was cut off, and the build itself finished and saved. The chat used to
  // show "Build failed" for a build that worked.
  it("recovers a build that finished after its request was cut off", async () => {
    const got = await recoverFinishedBuild(async () => ({
      steps: [],
      done: true,
      reply: "Built it. Test run answer: 42",
    }));
    expect(got).toBe("Built it. Test run answer: 42");
  });

  // Still running: the request failed for a real reason and the error stands.
  it("returns null while the build is still going", async () => {
    const got = await recoverFinishedBuild(async () => ({ steps: [], done: false }));
    expect(got).toBeNull();
  });

  // Done with no reply is a build that ended without an answer. There is
  // nothing to show, so the original error stands.
  it("returns null when the build finished with no answer", async () => {
    const got = await recoverFinishedBuild(async () => ({
      steps: [],
      done: true,
      reply: "",
    }));
    expect(got).toBeNull();
  });

  // The progress endpoint is a nicety. If it is also unreachable, the
  // original error is still what the user sees.
  it("returns null when the progress poll itself fails", async () => {
    const got = await recoverFinishedBuild(async () => {
      throw new Error("network");
    });
    expect(got).toBeNull();
  });
});
