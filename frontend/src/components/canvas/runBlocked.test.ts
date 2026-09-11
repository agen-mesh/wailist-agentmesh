import { describe, it, expect } from "vitest";
import { runBlockedMessage, runBlockedReason } from "./runBlocked";

// One case per row of the table, named for the situation a user is actually in.
describe("runBlockedMessage", () => {
  it("does not block a deployed workflow", () => {
    for (const hasProviderNode of [true, false]) {
      for (const canDeploy of [true, false]) {
        expect(
          runBlockedMessage({ deployed: true, hasProviderNode, canDeploy }),
        ).toBeNull();
      }
    }
  });

  it("tells an editor with an empty graph to add a provider, not to deploy", () => {
    const msg = runBlockedMessage({
      deployed: false,
      hasProviderNode: false,
      canDeploy: true,
    });
    expect(msg).toBe("Add a provider node before running");
    // The bug this fixes: "deploy" was the advice even with nothing to deploy.
    expect(msg).not.toMatch(/deploy first/i);
  });

  it("tells a viewer with an empty graph where the graph gets built", () => {
    const msg = runBlockedMessage({
      deployed: false,
      hasProviderNode: false,
      canDeploy: false,
    });
    expect(msg).toMatch(/no agent yet/i);
    expect(msg).toMatch(/desktop app/i);
    expect(msg).not.toMatch(/deploy first/i);
  });

  it("keeps the original wording for an editor who really does need to deploy", () => {
    expect(
      runBlockedMessage({
        deployed: false,
        hasProviderNode: true,
        canDeploy: true,
      }),
    ).toBe("Deploy first to run");
  });

  it("does not tell a viewer to press a Deploy button they do not have", () => {
    const msg = runBlockedMessage({
      deployed: false,
      hasProviderNode: true,
      canDeploy: false,
    });
    expect(msg).toMatch(/desktop app/i);
    expect(msg).not.toBe("Deploy first to run");
  });

  // Every blocked branch must actually say something -- an empty toast would
  // read as the click doing nothing at all.
  it("always returns a non-empty message when it blocks", () => {
    for (const hasProviderNode of [true, false]) {
      for (const canDeploy of [true, false]) {
        const msg = runBlockedMessage({
          deployed: false,
          hasProviderNode,
          canDeploy,
        });
        expect(msg).toBeTruthy();
        expect((msg ?? "").length).toBeGreaterThan(10);
      }
    }
  });
});

describe("runBlockedReason", () => {
  it("is null when the run can proceed", () => {
    expect(
      runBlockedReason({
        deployed: true,
        hasProviderNode: true,
        canDeploy: true,
      }),
    ).toBeNull();
  });

  it("names the missing provider, with no deploy action to offer", () => {
    const r = runBlockedReason({
      deployed: false,
      hasProviderNode: false,
      canDeploy: true,
    });
    expect(r?.code).toBe("no-provider");
    expect(r?.action).toBeNull();
    expect(r?.title).toBe("No model attached yet");
  });

  it("offers a deploy action to someone who may deploy", () => {
    const r = runBlockedReason({
      deployed: false,
      hasProviderNode: true,
      canDeploy: true,
    });
    expect(r?.code).toBe("not-deployed");
    expect(r?.action).toBe("deploy");
  });

  it("offers no deploy action to a read-only viewer", () => {
    const r = runBlockedReason({
      deployed: false,
      hasProviderNode: true,
      canDeploy: false,
    });
    expect(r?.code).toBe("not-deployed");
    expect(r?.action).toBeNull();
    expect(r?.detail).toContain("desktop app");
  });

  it("keeps runBlockedMessage in sync with the reason's detail", () => {
    const input = { deployed: false, hasProviderNode: true, canDeploy: true };
    expect(runBlockedMessage(input)).toBe(runBlockedReason(input)!.detail);
  });
});
