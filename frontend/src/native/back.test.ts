import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const app = vi.hoisted(() => ({
  handlers: [] as ((event: { canGoBack: boolean }) => void)[],
  minimizeApp: vi.fn(async () => {}),
}));

vi.mock("@capacitor/app", () => ({
  App: {
    addListener: async (
      _event: string,
      handler: (event: { canGoBack: boolean }) => void,
    ) => {
      app.handlers.push(handler);
      return { remove: async () => {} };
    },
    minimizeApp: app.minimizeApp,
  },
}));

beforeEach(() => {
  vi.resetModules();
  app.handlers.length = 0;
  app.minimizeApp.mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("listenForBack", () => {
  it("steps back through the app's history when there is some", async () => {
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { listenForBack } = await import("./back");
    await listenForBack();

    app.handlers[0]({ canGoBack: true });

    expect(back).toHaveBeenCalledTimes(1);
    expect(app.minimizeApp).not.toHaveBeenCalled();
  });

  it("sends the app to the background on the first screen", async () => {
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { listenForBack } = await import("./back");
    await listenForBack();

    app.handlers[0]({ canGoBack: false });

    expect(app.minimizeApp).toHaveBeenCalledTimes(1);
    expect(back).not.toHaveBeenCalled();
  });

  it("closes a modal history entry when native history reports no previous page", async () => {
    vi.spyOn(window.history, "state", "get").mockReturnValue({ __amSheet: true });
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { listenForBack } = await import("./back");
    await listenForBack();

    app.handlers[0]({ canGoBack: false });

    expect(back).toHaveBeenCalledTimes(1);
    expect(app.minimizeApp).not.toHaveBeenCalled();
  });

  it("minimizes after the modal history entry has been removed", async () => {
    vi.spyOn(window.history, "state", "get").mockReturnValue({ __amSheet: false });
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { listenForBack } = await import("./back");
    await listenForBack();

    app.handlers[0]({ canGoBack: false });

    expect(back).not.toHaveBeenCalled();
    expect(app.minimizeApp).toHaveBeenCalledTimes(1);
  });

  it("attaches one listener however often it is called", async () => {
    const { listenForBack } = await import("./back");
    await listenForBack();
    await listenForBack();
    expect(app.handlers).toHaveLength(1);
  });
});
