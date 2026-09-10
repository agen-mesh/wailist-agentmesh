import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { act } from "react";
// Type-only, so it is erased before the vi.mock of this module takes effect.
// Annotating the fake with the real return type is what stops a test setting a
// state the component can never actually be given.
import type { PushReadState } from "@/native/push";

// Driving the component, not its parts.
//
// The bug this file exists for was reported in review on #174: pressing "Turn
// off" cleared the opt-in, the sheet re-read its state, Android still said the
// permission was granted, and the panel snapped straight back to "Notifications
// are on". The unit tests around notificationState() now cover the boundary,
// but the PR description's own four-state walkthrough missed it entirely --
// because each state was set up DIRECTLY, and "granted, then turned off" is
// only reachable by a TRANSITION. Nothing short of driving the component gets
// there.
//
// So these tests move between states rather than asserting each one in
// isolation, and the fake shell below behaves the way Android actually does:
// turning notifications off in the app clears the opt-in and leaves the OS
// permission exactly where it was, because nothing in an app can revoke it.

type Perm = "granted" | "denied" | "prompt";

function fakeShell(initial: { perm: Perm; optedIn: boolean }) {
  const state = { ...initial };
  const calls = { enable: 0, disable: 0, settings: 0 };
  return {
    state,
    calls,
    shell: {
      async notificationState(): Promise<PushReadState> {
        if (state.perm === "denied") return "denied";
        if (state.perm !== "granted") return "off";
        return state.optedIn ? "granted" : "off";
      },
      async enableNotifications() {
        calls.enable += 1;
        // Mirrors enablePush: asks only if the question is still open, and
        // records the opt-in only once registration succeeds.
        if (state.perm === "prompt") state.perm = "granted";
        if (state.perm !== "granted") return "denied";
        state.optedIn = true;
        return "granted";
      },
      async disableNotifications() {
        calls.disable += 1;
        state.optedIn = false;
        // Deliberately does NOT touch state.perm. That is the whole point.
      },
    },
  };
}

let current = fakeShell({ perm: "prompt", optedIn: false });

vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: true }));
vi.mock("@/native", () => ({
  get shell() {
    return current.shell;
  },
}));
vi.mock("@/native/push", () => ({
  PUSH_DISCLOSURE: {
    title: "Tell me when a workflow finishes",
    body: "First paragraph.\n\nSecond paragraph.",
    grant: "Turn on notifications",
    decline: "Not now",
  },
}));
vi.mock("@/native/permissions", () => ({
  openSettings: async () => {
    current.calls.settings += 1;
  },
}));

let NotificationsSheet: typeof import("./NotificationsSheet").NotificationsSheet;

// The mount read is async and focus moves on the next task; let both land so
// no test asserts against the "loading" frame by accident.
async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function open(initial: { perm: Perm; optedIn: boolean }) {
  current = fakeShell(initial);
  const onClose = vi.fn();
  render(<NotificationsSheet onClose={onClose} />);
  await settle();
  return { onClose };
}

const heading = () => screen.getByRole("heading").textContent;
const button = (label: string) =>
  screen.getByRole("button", { name: label }) as HTMLButtonElement;

beforeEach(async () => {
  ({ NotificationsSheet } = await import("./NotificationsSheet"));
});

afterEach(() => {
  cleanup();
});

describe("the state the sheet opens on", () => {
  it("offers to turn notifications on when nothing has been decided", async () => {
    await open({ perm: "prompt", optedIn: false });
    expect(heading()).toBe("Tell me when a workflow finishes");
    expect(button("Turn on notifications")).toBeTruthy();
  });

  it("shows the on panel only when permission AND the opt-in agree", async () => {
    await open({ perm: "granted", optedIn: true });
    expect(heading()).toBe("Notifications are on");
    expect(button("Turn off")).toBeTruthy();
  });

  it("does not open on the on panel when permission is granted but the user opted out", async () => {
    // The second half of the report: a device that was switched off on an
    // earlier visit keeps its OS permission, so a permission-only read opened
    // straight into "Notifications are on".
    await open({ perm: "granted", optedIn: false });
    expect(heading()).toBe("Tell me when a workflow finishes");
    expect(screen.queryByRole("button", { name: "Turn off" })).toBeNull();
  });

  it("sends a refused device to Settings rather than offering a retry", async () => {
    await open({ perm: "denied", optedIn: false });
    expect(heading()).toBe("Notifications are off");
    expect(button("Open Settings")).toBeTruthy();
  });

  it("explains an unavailable device without offering Settings", async () => {
    // Nobody refused anything -- no Firebase, or no Play services -- so a
    // route to Settings would send the reader somewhere that cannot help.
    current = fakeShell({ perm: "prompt", optedIn: false });
    current.shell.notificationState = async () => "unavailable";
    render(<NotificationsSheet onClose={vi.fn()} />);
    await settle();
    expect(heading()).toBe("Notifications are not available here");
    expect(screen.queryByRole("button", { name: "Open Settings" })).toBeNull();
  });
});

describe("moving between states", () => {
  it("stays off after Turn off, with the OS permission still granted", async () => {
    // The reported bug, driven through the UI. disableNotifications clears the
    // opt-in and cannot revoke the permission, so a permission-only read
    // answered "granted" and the panel snapped back with a Turn off button on
    // it -- immediately after the user pressed Turn off.
    await open({ perm: "granted", optedIn: true });
    expect(heading()).toBe("Notifications are on");

    await act(async () => {
      button("Turn off").click();
    });

    await waitFor(() =>
      expect(heading()).toBe("Tell me when a workflow finishes"),
    );
    expect(screen.queryByRole("button", { name: "Turn off" })).toBeNull();
    expect(current.state.optedIn).toBe(false);
    // The permission is untouched, which is exactly why reading it alone was
    // not enough.
    expect(current.state.perm).toBe("granted");
  });

  it("turns on from the disclosure and lands on the on panel", async () => {
    await open({ perm: "prompt", optedIn: false });

    await act(async () => {
      button("Turn on notifications").click();
    });

    await waitFor(() => expect(heading()).toBe("Notifications are on"));
    expect(current.calls.enable).toBe(1);
    expect(current.state.optedIn).toBe(true);
  });

  it("survives a full off/on/off round trip", async () => {
    // Each leg re-reads rather than assuming, so a state that is only wrong on
    // the second pass would show up here.
    await open({ perm: "granted", optedIn: false });
    expect(heading()).toBe("Tell me when a workflow finishes");

    await act(async () => {
      button("Turn on notifications").click();
    });
    await waitFor(() => expect(heading()).toBe("Notifications are on"));

    await act(async () => {
      button("Turn off").click();
    });
    await waitFor(() =>
      expect(heading()).toBe("Tell me when a workflow finishes"),
    );
  });

  it("shows the refusal, not the on panel, when Android says no", async () => {
    await open({ perm: "denied", optedIn: false });
    await act(async () => {
      button("Open Settings").click();
    });
    await waitFor(() => expect(current.calls.settings).toBe(1));
    // Pressing it must not be treated as a retry that succeeded.
    expect(heading()).toBe("Notifications are off");
  });
});

describe("keyboard and focus", () => {
  it("moves focus into the panel, which the closing menu would otherwise steal", async () => {
    await open({ perm: "prompt", optedIn: false });
    const panel = screen.getByRole("dialog");
    expect(panel.contains(document.activeElement)).toBe(true);
  });

  it("closes on Escape", async () => {
    const { onClose } = await open({ perm: "prompt", optedIn: false });
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("hands focus back to the control it was given, not to the body", async () => {
    // The opener is a menu item that unmounts with its menu, so focusing
    // whatever had focus on mount is not enough -- the caller passes the
    // account button, which is still there.
    const trigger = document.createElement("button");
    document.body.appendChild(trigger);
    const returnFocusTo = { current: trigger };
    current = fakeShell({ perm: "prompt", optedIn: false });
    const onClose = vi.fn();
    render(
      <NotificationsSheet onClose={onClose} returnFocusTo={returnFocusTo} />,
    );
    await settle();

    await act(async () => {
      button("Not now").click();
    });

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  it("releases the scroll lock when it unmounts", async () => {
    // overflowY, not the `overflow` shorthand: useScrollLock writes the two
    // axes separately on purpose, so the shorthand reads back "" and an
    // assertion on it would pass whatever the hook did.
    const { unmount } = render(<NotificationsSheet onClose={vi.fn()} />);
    await settle();
    expect(document.body.style.overflowY).toBe("hidden");
    expect(document.documentElement.style.overflowY).toBe("hidden");
    unmount();
    expect(document.body.style.overflowY).toBe("");
    expect(document.documentElement.style.overflowY).toBe("");
  });
});
