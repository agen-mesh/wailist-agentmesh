import { describe, expect, it } from "vitest";
import { canPersist } from "./useChatSession";

describe("canPersist", () => {
  it("writes a transcript that loaded cleanly", () => {
    expect(canPersist(true, "wf1", "wf1")).toBe(true);
  });

  // The failure this exists for: a GET that 500s leaves the console empty,
  // and saving that empty console destroys the stored conversation.
  it("never writes after a failed load", () => {
    expect(canPersist(false, "wf1", "wf1")).toBe(false);
  });

  // A workflow switch: state still holds the previous conversation while the
  // new one loads, and the effect already sees the new id.
  it("never writes one workflow's transcript under another's id", () => {
    expect(canPersist(true, "wf1", "wf2")).toBe(false);
    expect(canPersist(true, undefined, "wf2")).toBe(false);
  });

  it("does nothing without a workflow", () => {
    expect(canPersist(true, undefined, undefined)).toBe(false);
  });
});
