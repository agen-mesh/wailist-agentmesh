import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

// In the app, Android reports the keyboard opening and closing, and the bar
// follows that report rather than guessing from the viewport. The reviewer's
// case is the one a guess gets wrong: a rotation that closes the keyboard while
// the field keeps focus.
const keyboard = vi.hoisted(() => ({
  onChange: null as null | ((open: boolean) => void),
  stopped: 0,
}));
vi.mock("@/native/keyboard", () => ({
  watchKeyboard: (onChange: (open: boolean) => void) => {
    keyboard.onChange = onChange;
    return () => {
      keyboard.stopped += 1;
    };
  },
}));
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => true }));
vi.mock("next/navigation", () => ({ usePathname: () => "/workflows" }));

import { BottomNav } from "./BottomNav";

const typing = () => document.body.hasAttribute("data-typing");
const report = (open: boolean) => act(() => keyboard.onChange!(open));
function resize(width: number, height: number) {
  Object.defineProperty(window, "innerWidth", {
    value: width,
    configurable: true,
  });
  Object.defineProperty(window, "innerHeight", {
    value: height,
    configurable: true,
  });
  act(() => {
    window.dispatchEvent(new Event("resize"));
  });
}

afterEach(() => {
  cleanup();
  keyboard.onChange = null;
  keyboard.stopped = 0;
  document.body.removeAttribute("data-typing");
});

function renderWithSearch() {
  const view = render(
    <>
      <input type="search" aria-label="Search workflows" />
      <BottomNav />
    </>,
  );
  const field = screen.getByRole("searchbox", { name: "Search workflows" });
  act(() => {
    field.focus();
    fireEvent.focusIn(field);
  });
  return view;
}

describe("BottomNav in the app", () => {
  it("steps aside while Android says the keyboard is up", () => {
    renderWithSearch();
    expect(typing()).toBe(false);

    report(true);
    expect(typing()).toBe(true);

    // Back closes the keyboard and leaves the field focused.
    report(false);
    expect(typing()).toBe(false);
  });

  it("returns when a rotation closes the keyboard and focus stays", () => {
    renderWithSearch();
    report(true);
    // Landscape: shorter than the portrait height even with no keyboard,
    // which is what fooled the measured version.
    resize(900, 300);
    expect(typing()).toBe(true);

    report(false);
    expect(typing()).toBe(false);
  });

  it("stays aside through a rotation the keyboard survives", () => {
    renderWithSearch();
    report(true);
    resize(900, 150);
    resize(900, 400);
    expect(typing()).toBe(true);
  });

  it("stops listening when the bar goes away", () => {
    const { unmount } = renderWithSearch();
    unmount();
    expect(keyboard.stopped).toBe(1);
  });
});
