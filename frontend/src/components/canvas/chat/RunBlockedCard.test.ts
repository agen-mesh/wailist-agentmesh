import { describe, it, expect } from "vitest";
import { runBlockedCardCopy } from "./RunBlockedCard";

describe("runBlockedCardCopy", () => {
  it("labels the deploy action for someone who may deploy", () => {
    const copy = runBlockedCardCopy({
      code: "not-deployed",
      title: "Not deployed",
      detail: "Deploy first to run",
      action: "deploy",
    });
    expect(copy.actionLabel).toBe("Deploy now");
  });

  it("has no action label when there is nothing to click", () => {
    const copy = runBlockedCardCopy({
      code: "not-ready",
      title: "No model attached yet",
      detail: "Add a provider node before running",
      action: null,
    });
    expect(copy.actionLabel).toBeNull();
  });

  it("uses the error tone for a deployable-but-undeployed workflow", () => {
    const copy = runBlockedCardCopy({
      code: "not-deployed",
      title: "Not deployed",
      detail: "Deploy first to run",
      action: "deploy",
    });
    expect(copy.tone).toBe("error");
  });

  it("uses the warm tone for a workflow still being built", () => {
    const copy = runBlockedCardCopy({
      code: "not-ready",
      title: "No model attached yet",
      detail: "Add a provider node before running",
      action: null,
    });
    expect(copy.tone).toBe("warn");
  });
});
