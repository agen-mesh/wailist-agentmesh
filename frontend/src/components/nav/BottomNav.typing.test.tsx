import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

// The bar steps aside while the on-screen keyboard is up, so it does not sit
// over the field being typed in -- and only then. A field can keep focus after
// the keyboard is closed (Back on Android does exactly that), and the bar must
// come back when it does.
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => true }));
vi.mock("next/navigation", () => ({ usePathname: () => "/workflows" }));

import { BottomNav } from "./BottomNav";

const FULL = 800;
// The WebView shrinks to the space above the keyboard when it opens.
function setViewportHeight(height: number) {
  Object.defineProperty(window, "innerHeight", {
    value: height,
    configurable: true,
  });
  act(() => {
    window.dispatchEvent(new Event("resize"));
  });
}
const typing = () => document.body.hasAttribute("data-typing");
// Rotation: the width changes, and with the keyboard still open the height
// arrives already shrunk.
function rotate(width: number, height: number) {
  Object.defineProperty(window, "innerWidth", {
    value: width,
    configurable: true,
  });
  setViewportHeight(height);
}
const WIDTH = window.innerWidth;

beforeEach(() => {
  Object.defineProperty(window, "innerWidth", {
    value: WIDTH,
    configurable: true,
  });
  setViewportHeight(FULL);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  document.body.removeAttribute("data-typing");
});

function renderWithSearch() {
  render(
    <>
      <input type="search" aria-label="Search workflows" />
      <button type="button">Elsewhere</button>
      <BottomNav />
    </>,
  );
  const field = screen.getByRole("searchbox", { name: "Search workflows" });
  act(() => {
    field.focus();
    fireEvent.focusIn(field);
  });
  return field;
}

describe("BottomNav while typing", () => {
  // The bug: focus alone hid the bar, so it stayed gone after the keyboard
  // was put away with the field still focused.
  it("stays while a field has focus but the keyboard is not up", () => {
    renderWithSearch();
    expect(typing()).toBe(false);
  });

  it("steps aside while the keyboard is up, and returns when it closes", () => {
    renderWithSearch();
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(true);

    // Back closes the keyboard; the field keeps focus.
    setViewportHeight(FULL);
    expect(typing()).toBe(false);
  });

  it("returns when focus leaves the field", () => {
    vi.useFakeTimers();
    const field = renderWithSearch();
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(true);

    act(() => {
      screen.getByRole("button", { name: "Elsewhere" }).focus();
      fireEvent.focusOut(field);
      vi.runAllTimers();
    });
    expect(typing()).toBe(false);
  });

  // Rotating with the keyboard open used to take the shrunken height as the
  // new full height, so the bar came back above the still-open keyboard.
  it("stays aside through a rotation while the keyboard is open", () => {
    renderWithSearch();
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(true);

    rotate(WIDTH + 400, 400 - 200);
    expect(typing()).toBe(true);

    // The keyboard closes in the new orientation: the height grows back.
    setViewportHeight(400);
    expect(typing()).toBe(false);
  });

  // The converse: the rotation closes the keyboard and the field keeps focus.
  // The landscape height with no keyboard is still below the portrait height
  // with one, so nothing ever "grew back" and the bar stayed away. A width
  // seen before now has its own full height to measure against.
  it("returns when a rotation closes the keyboard in an orientation seen before", () => {
    renderWithSearch();
    rotate(WIDTH + 400, 400);
    rotate(WIDTH, FULL);
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(true);

    rotate(WIDTH + 400, 400);
    expect(typing()).toBe(false);
  });

  it("ignores a small resize that is not a keyboard", () => {
    renderWithSearch();
    setViewportHeight(FULL - 60);
    expect(typing()).toBe(false);
  });

  it("stays put for a control that does not bring up the keyboard", () => {
    render(
      <>
        <input type="checkbox" aria-label="Agree" />
        <BottomNav />
      </>,
    );
    const box = screen.getByRole("checkbox", { name: "Agree" });
    act(() => {
      box.focus();
      fireEvent.focusIn(box);
    });
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(false);
  });

  it("clears the attribute when the bar goes away", () => {
    const { unmount } = render(
      <>
        <input type="text" aria-label="Name" />
        <BottomNav />
      </>,
    );
    const field = screen.getByRole("textbox", { name: "Name" });
    act(() => {
      field.focus();
      fireEvent.focusIn(field);
    });
    setViewportHeight(FULL - 320);
    expect(typing()).toBe(true);

    unmount();
    expect(typing()).toBe(false);
  });
});
