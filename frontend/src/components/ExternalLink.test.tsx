import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

const state = vi.hoisted(() => ({
  native: false,
  openExternal: vi.fn<(url: string) => Promise<void>>(async () => {}),
}));

vi.mock("@/lib/nativeAuth", () => ({
  get IS_NATIVE() {
    return state.native;
  },
}));
vi.mock("@/lib/openExternal", () => ({ openExternal: state.openExternal }));

import { ExternalLink } from "./ExternalLink";

afterEach(() => {
  cleanup();
  state.native = false;
  state.openExternal.mockClear();
});

function clickLink() {
  const link = screen.getByRole("link", { name: "TX1" });
  const event = createEvent.click(link);
  fireEvent(link, event);
  return { link, event };
}

describe("ExternalLink", () => {
  it("is an ordinary new-tab link on the web", () => {
    render(
      <ExternalLink href="https://explorer.example/tx/1">TX1</ExternalLink>,
    );

    const { link, event } = clickLink();

    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(event.defaultPrevented).toBe(false);
    expect(state.openExternal).not.toHaveBeenCalled();
  });

  it("opens an in-app browser tab in the Android app", () => {
    state.native = true;
    render(
      <ExternalLink href="https://explorer.example/tx/1">TX1</ExternalLink>,
    );

    const { event } = clickLink();

    expect(event.defaultPrevented).toBe(true);
    expect(state.openExternal).toHaveBeenCalledWith(
      "https://explorer.example/tx/1",
    );
  });
});
