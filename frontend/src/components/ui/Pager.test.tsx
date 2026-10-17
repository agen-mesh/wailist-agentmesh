import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Pager, pageSlice } from "./Pager";

afterEach(cleanup);

const items = Array.from({ length: 23 }, (_, i) => i);

describe("pageSlice", () => {
  it("returns at most one page of rows", () => {
    expect(pageSlice(items, 0, 10).rows).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(pageSlice(items, 2, 10).rows).toEqual([20, 21, 22]);
    expect(pageSlice(items, 0, 10).totalPages).toBe(3);
  });

  // The reason the caller renders `safePage` rather than its own `page`: a
  // filter that shortens the list must not strand the reader on an empty page.
  it("clamps a page that no longer exists onto the last one", () => {
    const { rows, page, totalPages } = pageSlice(items.slice(0, 4), 7, 10);
    expect(page).toBe(0);
    expect(totalPages).toBe(1);
    expect(rows).toHaveLength(4);
  });

  it("never returns a negative page", () => {
    expect(pageSlice(items, -3, 10).page).toBe(0);
  });

  it("reports one page for an empty list rather than zero", () => {
    expect(pageSlice([], 0, 10)).toEqual({ rows: [], page: 0, totalPages: 1 });
  });
});

describe("Pager", () => {
  const props = {
    page: 1,
    totalPages: 3,
    total: 23,
    pageSize: 10,
    onPage: vi.fn(),
    onPageSize: vi.fn(),
    noun: "workflows",
  };

  it("says where the reader is and how much there is", () => {
    render(<Pager {...props} />);
    expect(screen.getByText("Page 2 of 3")).toBeTruthy();
    expect(screen.getByText(/23 workflows/)).toBeTruthy();
  });

  it("steps one page at a time in each direction", () => {
    const onPage = vi.fn();
    render(<Pager {...props} onPage={onPage} />);
    fireEvent.click(screen.getByRole("button", { name: /next/i }));
    expect(onPage).toHaveBeenCalledWith(2);
    fireEvent.click(screen.getByRole("button", { name: /prev/i }));
    expect(onPage).toHaveBeenCalledWith(0);
  });

  it("disables the end it is already at", () => {
    render(<Pager {...props} page={0} />);
    expect(screen.getByRole("button", { name: /prev/i })).toHaveProperty(
      "disabled",
      true,
    );
    cleanup();
    render(<Pager {...props} page={2} />);
    expect(screen.getByRole("button", { name: /next/i })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("names the page-size control after the thing being paged", () => {
    const onPageSize = vi.fn();
    render(<Pager {...props} onPageSize={onPageSize} />);
    const select = screen.getByLabelText("workflows per page");
    fireEvent.change(select, { target: { value: "5" } });
    expect(onPageSize).toHaveBeenCalledWith(5);
  });
});
