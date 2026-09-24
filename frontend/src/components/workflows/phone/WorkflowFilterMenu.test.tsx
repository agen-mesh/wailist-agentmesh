import { afterEach, describe, expect, it } from "vitest";
import { useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Sort, StatusFilter } from "@/lib/workflowList";
import { WorkflowFilterMenu } from "./WorkflowFilterMenu";

// The menu is controlled, so it is tested inside a parent that holds the
// state the way WorkflowsPage does.
function Harness() {
  const [status, setStatus] = useState<StatusFilter>("all");
  const [sort, setSort] = useState<Sort>(null);
  return (
    <div>
      <WorkflowFilterMenu
        status={status}
        onStatusChange={setStatus}
        sort={sort}
        onSortChange={setSort}
      />
      <output data-testid="state">
        {status}|{sort ? `${sort.key}:${sort.dir}` : "none"}
      </output>
      <p>Outside</p>
    </div>
  );
}

const state = () => screen.getByTestId("state").textContent;
const trigger = () => screen.getByRole("button", { name: /Filter and sort/ });
const menu = () =>
  screen.queryByRole("group", { name: "Filter and sort options" });
const item = (name: RegExp) => screen.getByRole("button", { name });

afterEach(cleanup);

describe("WorkflowFilterMenu", () => {
  it("opens as a dropdown with the show and sort options", () => {
    render(<Harness />);
    expect(menu()).toBeNull();
    fireEvent.click(trigger());
    expect(menu()).toBeTruthy();
    expect(trigger().getAttribute("aria-expanded")).toBe("true");
    expect(trigger().getAttribute("aria-controls")).toBe(menu()!.id);
    expect(
      screen.getByRole("group", { name: "Show" }).querySelectorAll("button"),
    ).toHaveLength(4);
    expect(
      screen.getByRole("group", { name: "Sort by" }).querySelectorAll("button"),
    ).toHaveLength(5);
  });

  it("applies a choice and stays open for the next one", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.click(item(/^Deployed/));
    fireEvent.click(item(/^Alphabetical/));
    expect(state()).toBe("deployed|alpha:asc");
    expect(menu()).toBeTruthy();
    expect(item(/^Deployed/).getAttribute("aria-pressed")).toBe("true");
  });

  it("flips Highest cost to Lowest cost on a second tap", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.click(item(/^Highest cost/));
    expect(state()).toBe("all|cost:desc");
    expect(item(/^Highest cost, high → low/)).toBeTruthy();
    fireEvent.click(item(/^Highest cost/));
    expect(state()).toBe("all|cost:asc");
    expect(item(/^Lowest cost/).getAttribute("aria-pressed")).toBe("true");
  });

  it("closes the moment the page scrolls", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.scroll(window);
    expect(menu()).toBeNull();
  });

  // A list that fits the screen never scrolls, so the drag itself has to
  // close the menu.
  it("closes the moment a drag or wheel starts outside it", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.touchMove(item(/^Paused/));
    expect(menu()).toBeTruthy();
    fireEvent.touchMove(screen.getByText("Outside"));
    expect(menu()).toBeNull();

    fireEvent.click(trigger());
    fireEvent.wheel(screen.getByText("Outside"));
    expect(menu()).toBeNull();
  });

  it("closes on a tap outside, but not on a tap inside", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.pointerDown(item(/^Paused/));
    expect(menu()).toBeTruthy();
    fireEvent.pointerDown(screen.getByText("Outside"));
    expect(menu()).toBeNull();
  });

  it("closes on Escape and hands focus back to the trigger", () => {
    render(<Harness />);
    fireEvent.click(trigger());
    fireEvent.keyDown(document, { key: "Escape" });
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(trigger());
  });

  it("marks the trigger once anything differs from the default", () => {
    render(<Harness />);
    expect(trigger().getAttribute("aria-label")).toBe("Filter and sort");
    fireEvent.click(trigger());
    fireEvent.click(item(/^Recently run/));
    expect(trigger().getAttribute("aria-label")).toBe(
      "Filter and sort, changed",
    );
  });

  // Plain buttons, not a menu: nothing promises arrow-key handling that
  // isn't there, and Tab reaches every option in order.
  it("uses toggle buttons rather than ARIA menu roles", () => {
    render(<Harness />);
    expect(trigger().hasAttribute("aria-haspopup")).toBe(false);
    fireEvent.click(trigger());
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.queryAllByRole("menuitemradio")).toHaveLength(0);
    expect(item(/^All$/).getAttribute("aria-pressed")).toBe("true");
    expect(item(/^Paused/).getAttribute("aria-pressed")).toBe("false");
  });
});
