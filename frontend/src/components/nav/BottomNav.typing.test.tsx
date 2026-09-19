import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

// The bar steps aside while the on-screen keyboard is up, so it does not sit
// over the field being typed in.
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => true }));
vi.mock("next/navigation", () => ({ usePathname: () => "/workflows" }));

import { BottomNav } from "./BottomNav";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  document.body.removeAttribute("data-typing");
});

describe("BottomNav while typing", () => {
  it("steps aside while a text field has focus and returns after", () => {
    vi.useFakeTimers();
    render(
      <>
        <input type="search" aria-label="Search workflows" />
        <button type="button">Elsewhere</button>
        <BottomNav />
      </>,
    );
    const field = screen.getByRole("searchbox", { name: "Search workflows" });
    const other = screen.getByRole("button", { name: "Elsewhere" });

    act(() => {
      field.focus();
      fireEvent.focusIn(field);
    });
    expect(document.body.hasAttribute("data-typing")).toBe(true);

    act(() => {
      other.focus();
      fireEvent.focusOut(field);
      vi.runAllTimers();
    });
    expect(document.body.hasAttribute("data-typing")).toBe(false);
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
    expect(document.body.hasAttribute("data-typing")).toBe(false);
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
    expect(document.body.hasAttribute("data-typing")).toBe(true);

    unmount();
    expect(document.body.hasAttribute("data-typing")).toBe(false);
  });
});
