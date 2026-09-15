import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ChatModeSwitch } from "./ChatModeSwitch";

afterEach(cleanup);

describe("ChatModeSwitch", () => {
  // The control it replaces was one pill labelled with the mode it was
  // already in, so it never said what it switched between.
  it("shows both modes and marks the active one", () => {
    render(
      <ChatModeSwitch
        buildMode
        onSelect={() => {}}
        hasChatTrigger
        canRun
      />,
    );
    expect(screen.getByRole("tab", { name: "Build" })).toHaveProperty(
      "ariaSelected",
      "true",
    );
    expect(screen.getByRole("tab", { name: "Run" })).toHaveProperty(
      "ariaSelected",
      "false",
    );
  });

  it("switches only when the other mode is chosen", () => {
    const onSelect = vi.fn();
    render(
      <ChatModeSwitch
        buildMode
        onSelect={onSelect}
        hasChatTrigger
        canRun
      />,
    );
    screen.getByRole("tab", { name: "Build" }).click();
    expect(onSelect).not.toHaveBeenCalled();
    screen.getByRole("tab", { name: "Run" }).click();
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  // Without a chat trigger a run carries no message, so there is nothing to
  // talk to and the reason has to say so rather than leaving Run dead.
  it("refuses Run without a chat trigger and explains why", () => {
    const onSelect = vi.fn();
    render(
      <ChatModeSwitch
        buildMode
        onSelect={onSelect}
        hasChatTrigger={false}
        canRun
      />,
    );
    const run = screen.getByRole("tab", { name: "Run" });
    expect(run).toHaveProperty("disabled", true);
    run.click();
    expect(onSelect).not.toHaveBeenCalled();
    // The reason rides on the control itself now that the switch sits in the
    // header row with no space for a caption.
    expect(run.getAttribute("title")).toMatch(/Add a Chat trigger/);
  });

  it("refuses Run while there is nothing to run", () => {
    render(
      <ChatModeSwitch
        buildMode
        onSelect={() => {}}
        hasChatTrigger
        canRun={false}
      />,
    );
    expect(screen.getByRole("tab", { name: "Run" })).toHaveProperty(
      "disabled",
      true,
    );
  });
});
