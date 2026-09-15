import { describe, expect, it } from "vitest";
import { canPersist } from "./useChatSession";

describe("canPersist", () => {
  it("writes a transcript that loaded cleanly", () => {
    expect(canPersist({ workflowId: "wf1", mode: "build", writable: true })).toBe(
      true,
    );
  });

  // The failure this exists for: a GET that 500s leaves the console empty,
  // and saving that empty console destroys the stored conversation.
  it("never writes after a failed load", () => {
    expect(
      canPersist({ workflowId: "wf1", mode: "build", writable: false }),
    ).toBe(false);
  });

  it("never writes before any conversation has loaded", () => {
    expect(canPersist(null)).toBe(false);
  });
});
