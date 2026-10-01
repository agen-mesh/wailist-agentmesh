import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  native: false,
  open: vi.fn<(options: { url: string }) => Promise<void>>(async () => {}),
  remove: vi.fn(async () => {}),
  handlers: [] as (() => void)[],
}));

vi.mock("@/lib/nativeAuth", () => ({
  get IS_NATIVE() {
    return state.native;
  },
}));
vi.mock("@capacitor/browser", () => ({
  Browser: {
    open: state.open,
    addListener: async (_event: string, handler: () => void) => {
      state.handlers.push(handler);
      return { remove: state.remove };
    },
  },
}));

import { openExternal } from "./openExternal";

beforeEach(() => {
  state.native = false;
  state.open.mockClear();
  state.remove.mockClear();
  state.handlers.length = 0;
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("openExternal", () => {
  it("opens a new tab on the web", async () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);

    await openExternal("https://explorer.example/tx/1");

    expect(open).toHaveBeenCalledWith(
      "https://explorer.example/tx/1",
      "_blank",
      "noopener,noreferrer",
    );
    expect(state.open).not.toHaveBeenCalled();
  });

  it("opens an in-app browser tab in the Android app", async () => {
    state.native = true;
    const open = vi.spyOn(window, "open").mockImplementation(() => null);

    await openExternal("https://explorer.example/tx/1");

    expect(state.open).toHaveBeenCalledWith({
      url: "https://explorer.example/tx/1",
    });
    expect(open).not.toHaveBeenCalled();
    expect(state.handlers).toHaveLength(0);
  });

  it("calls onClose once when the tab closes, and stops listening", async () => {
    state.native = true;
    const onClose = vi.fn();

    await openExternal("https://www.agent-mesh.app/billing", { onClose });
    expect(onClose).not.toHaveBeenCalled();

    state.handlers[0]();

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(state.remove).toHaveBeenCalledTimes(1);
  });
});
