import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import { useCloseOnBack } from "./useCloseOnBack";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("useCloseOnBack", () => {
  it("adds one history entry for the sheet, keeping the URL and state", () => {
    window.history.replaceState({ page: 1 }, "");
    const push = vi.spyOn(window.history, "pushState");
    renderHook(() => useCloseOnBack(() => {}));

    expect(push).toHaveBeenCalledTimes(1);
    expect(push.mock.calls[0][0]).toEqual({ page: 1, __amSheet: true });
    expect(push.mock.calls[0][2]).toBeUndefined();
  });

  it("closes the sheet when Back takes that entry off", () => {
    const onClose = vi.fn();
    renderHook(() => useCloseOnBack(onClose));

    window.dispatchEvent(new PopStateEvent("popstate"));
    window.dispatchEvent(new PopStateEvent("popstate"));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("removes the entry when the sheet closes itself, and closes once", () => {
    const onClose = vi.fn();
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {});
    const { result } = renderHook(() => useCloseOnBack(onClose));

    result.current();
    // The pop that history.back() causes must not close it a second time.
    window.dispatchEvent(new PopStateEvent("popstate"));

    expect(back).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not add a second entry when one is already there", () => {
    window.history.replaceState({ __amSheet: true }, "");
    const push = vi.spyOn(window.history, "pushState");
    renderHook(() => useCloseOnBack(() => {}));
    expect(push).not.toHaveBeenCalled();
  });
});
