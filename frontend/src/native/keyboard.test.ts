import { afterEach, describe, expect, it, vi } from "vitest";

// watchKeyboard forwards Android's own keyboard events, and is inert on the
// web so the caller keeps measuring the viewport there.
const plugin = vi.hoisted(() => ({
  listeners: new Map<string, () => void>(),
  removed: [] as string[],
}));
vi.mock("@capacitor/keyboard", () => ({
  Keyboard: {
    addListener: async (event: string, fn: () => void) => {
      plugin.listeners.set(event, fn);
      return {
        remove: async () => {
          plugin.removed.push(event);
        },
      };
    },
  },
}));

async function load(native: boolean) {
  vi.stubEnv("NEXT_PUBLIC_NATIVE_CLIENT", native ? "1" : "");
  vi.resetModules();
  return import("./keyboard");
}
const settle = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => {
  vi.unstubAllEnvs();
  plugin.listeners.clear();
  plugin.removed = [];
});

describe("watchKeyboard", () => {
  it("is not available on the web", async () => {
    const { watchKeyboard } = await load(false);
    expect(watchKeyboard(() => {})).toBeNull();
  });

  it("reports the keyboard opening and closing, and stops when asked", async () => {
    const { watchKeyboard } = await load(true);
    const seen: boolean[] = [];
    const stop = watchKeyboard((open) => seen.push(open));
    await settle();

    plugin.listeners.get("keyboardWillShow")!();
    plugin.listeners.get("keyboardWillHide")!();
    expect(seen).toEqual([true, false]);

    stop!();
    await settle();
    expect(plugin.removed.sort()).toEqual([
      "keyboardWillHide",
      "keyboardWillShow",
    ]);
  });

  it("removes listeners that arrive after it was already stopped", async () => {
    const { watchKeyboard } = await load(true);
    const stop = watchKeyboard(() => {});
    stop!();
    await settle();
    expect(plugin.removed).toHaveLength(2);
  });
});
