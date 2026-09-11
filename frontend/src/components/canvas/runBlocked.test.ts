import { describe, it, expect } from "vitest";
import { runBlockedMessage, runBlockedReason } from "./runBlocked";

const ready = { graphReady: true, agentMissingModel: false };
const unwired = { graphReady: false, agentMissingModel: false };
const noModel = { graphReady: false, agentMissingModel: true };

// One case per row of the table, named for the situation a user is actually in.
describe("runBlockedMessage", () => {
  it("does not block a deployed workflow", () => {
    for (const g of [ready, unwired, noModel]) {
      for (const canDeploy of [true, false]) {
        expect(runBlockedMessage({ deployed: true, canDeploy, ...g })).toBeNull();
      }
    }
  });

  it("tells an editor whose agent has no model to attach one, not to deploy", () => {
    const msg = runBlockedMessage({ deployed: false, canDeploy: true, ...noModel });
    expect(msg).toMatch(/provider/i);
    // The bug this fixes: "deploy" was the advice even with nothing to deploy.
    expect(msg).not.toMatch(/deploy first/i);
  });

  it("tells an editor with an unwired graph to connect a step, not to add a model", () => {
    const msg = runBlockedMessage({ deployed: false, canDeploy: true, ...unwired });
    expect(msg).toMatch(/connect/i);
    // A graph with no agent needs no model -- naming one would send the user
    // looking for something the workflow does not need.
    expect(msg).not.toMatch(/provider|model/i);
    expect(msg).not.toMatch(/deploy first/i);
  });

  it("tells a viewer with an unfinished graph where the graph gets built", () => {
    const msg = runBlockedMessage({ deployed: false, canDeploy: false, ...noModel });
    expect(msg).toMatch(/desktop app/i);
    expect(msg).not.toMatch(/deploy first/i);
  });

  it("keeps the original wording for an editor who really does need to deploy", () => {
    expect(runBlockedMessage({ deployed: false, canDeploy: true, ...ready })).toBe(
      "Deploy first to run",
    );
  });

  // A ready pipeline with no agent at all is blocked only by deployment --
  // exactly what the builder now produces for a fetch-and-save request.
  it("says deploy, not 'no model', for a ready graph with no agent", () => {
    const r = runBlockedReason({ deployed: false, canDeploy: true, ...ready });
    expect(r?.code).toBe("not-deployed");
    expect(r?.title).not.toMatch(/model/i);
  });

  it("does not tell a viewer to press a Deploy button they do not have", () => {
    const msg = runBlockedMessage({ deployed: false, canDeploy: false, ...ready });
    expect(msg).toMatch(/desktop app/i);
    expect(msg).not.toBe("Deploy first to run");
  });

  // Every blocked branch must actually say something -- an empty toast would
  // read as the click doing nothing at all.
  it("always returns a non-empty message when it blocks", () => {
    for (const g of [ready, unwired, noModel]) {
      for (const canDeploy of [true, false]) {
        const msg = runBlockedMessage({ deployed: false, canDeploy, ...g });
        expect(msg).toBeTruthy();
        expect((msg ?? "").length).toBeGreaterThan(10);
      }
    }
  });
});

describe("runBlockedReason", () => {
  it("is null when the run can proceed", () => {
    expect(runBlockedReason({ deployed: true, canDeploy: true, ...ready })).toBeNull();
  });

  it("names the missing model, with no deploy action to offer", () => {
    const r = runBlockedReason({ deployed: false, canDeploy: true, ...noModel });
    expect(r?.code).toBe("not-ready");
    expect(r?.action).toBeNull();
    expect(r?.title).toBe("No model attached yet");
  });

  it("names an unwired graph as nothing to run, with no deploy action", () => {
    const r = runBlockedReason({ deployed: false, canDeploy: true, ...unwired });
    expect(r?.code).toBe("not-ready");
    expect(r?.action).toBeNull();
    expect(r?.title).toBe("Nothing to run yet");
  });

  // Precise, per the failed Nifty/Sensex run: the graph had plenty of steps,
  // one of them just had nothing flowing into it. "Nothing to run yet" would
  // be wrong; name the step.
  it("names the step that has nothing flowing into it", () => {
    const r = runBlockedReason({
      deployed: false,
      canDeploy: true,
      graphReady: false,
      agentMissingModel: false,
      unreachedStep: "Extract Sensex Price",
    });
    expect(r?.code).toBe("not-ready");
    expect(r?.title).toBe("A step isn't connected");
    expect(r?.detail).toContain("Extract Sensex Price");
  });

  it("offers a deploy action to someone who may deploy", () => {
    const r = runBlockedReason({ deployed: false, canDeploy: true, ...ready });
    expect(r?.code).toBe("not-deployed");
    expect(r?.action).toBe("deploy");
  });

  it("offers no deploy action to a read-only viewer", () => {
    const r = runBlockedReason({ deployed: false, canDeploy: false, ...ready });
    expect(r?.code).toBe("not-deployed");
    expect(r?.action).toBeNull();
    expect(r?.detail).toContain("desktop app");
  });

  it("keeps runBlockedMessage in sync with the reason's detail", () => {
    const input = { deployed: false, canDeploy: true, ...ready };
    expect(runBlockedMessage(input)).toBe(runBlockedReason(input)!.detail);
  });
});
