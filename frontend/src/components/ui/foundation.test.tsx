import { createRef } from "react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Button, type ButtonProps } from "./button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select";

const scrollDescriptor = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  "scrollIntoView",
);

beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value: vi.fn(),
  });
});

afterEach(cleanup);

afterAll(() => {
  vi.unstubAllGlobals();
  if (scrollDescriptor) {
    Object.defineProperty(
      HTMLElement.prototype,
      "scrollIntoView",
      scrollDescriptor,
    );
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  }
});

function Example({
  value,
  disabled,
  onValueChange,
}: {
  value?: string;
  disabled?: boolean;
  onValueChange: (value: string) => void;
}) {
  return (
    <Select
      defaultValue="alpha"
      value={value}
      disabled={disabled}
      onValueChange={onValueChange}
    >
      <SelectTrigger aria-label="Selection">
        <SelectValue placeholder="Choose an option" />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectItem value="alpha">Alpha</SelectItem>
          <SelectItem value="beta" disabled>
            Beta
          </SelectItem>
          <SelectItem value="gamma">Gamma</SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

describe("shared foundation consumers", () => {
  it("accepts the original ButtonProps contract and forwards props and a ref", () => {
    const onClick = vi.fn();
    const ref = createRef<HTMLButtonElement>();
    const props: ButtonProps = {
      variant: "outline",
      size: "sm",
      asChild: false,
      "aria-label": "Foundation action",
      onClick,
    };
    render(
      <Button {...props} ref={ref}>
        Action
      </Button>,
    );

    const button = screen.getByRole("button", { name: "Foundation action" });
    expect(ref.current).toBe(button);
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("retains the expanded Button sizes and child composition contract", () => {
    const onClick = vi.fn();
    const props: ButtonProps = {
      variant: "ghost",
      size: "icon-sm",
      asChild: true,
      onClick,
    };
    render(
      <Button {...props}>
        <a href="#details" aria-label="Open details">
          Open
        </a>
      </Button>,
    );

    const link = screen.getByRole("link", { name: "Open details" });
    expect(link.getAttribute("href")).toBe("#details");
    expect(link.getAttribute("data-size")).toBe("icon-sm");
    expect(screen.queryByRole("button")).toBeNull();
    fireEvent.click(link);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("skips disabled options, selects by keyboard and restores trigger focus", async () => {
    const onValueChange = vi.fn();
    render(<Example onValueChange={onValueChange} />);
    const trigger = screen.getByRole("combobox", { name: "Selection" });
    fireEvent.keyDown(trigger, { key: "ArrowDown" });

    const alpha = await screen.findByRole("option", { name: "Alpha" });
    await waitFor(() => expect(document.activeElement).toBe(alpha));
    fireEvent.keyDown(alpha, { key: "ArrowDown" });
    const gamma = screen.getByRole("option", { name: "Gamma" });
    await waitFor(() => expect(document.activeElement).toBe(gamma));
    fireEvent.keyDown(gamma, { key: "Enter" });

    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(onValueChange).toHaveBeenCalledExactlyOnceWith("gamma");
    expect(trigger.textContent).toContain("Gamma");
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it("requests a controlled selection without overwriting the parent value", async () => {
    const onValueChange = vi.fn();
    const view = render(<Example value="alpha" onValueChange={onValueChange} />);
    const trigger = screen.getByRole("combobox", { name: "Selection" });
    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    const alpha = await screen.findByRole("option", { name: "Alpha" });
    await waitFor(() => expect(document.activeElement).toBe(alpha));
    fireEvent.keyDown(alpha, { key: "ArrowDown" });
    const gamma = screen.getByRole("option", { name: "Gamma" });
    await waitFor(() => expect(document.activeElement).toBe(gamma));
    fireEvent.keyDown(gamma, { key: "Enter" });

    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(onValueChange).toHaveBeenCalledExactlyOnceWith("gamma");
    expect(trigger.textContent).toContain("Alpha");
    await waitFor(() => expect(document.activeElement).toBe(trigger));

    view.rerender(<Example value="gamma" onValueChange={onValueChange} />);
    expect(trigger.textContent).toContain("Gamma");
    expect(onValueChange).toHaveBeenCalledOnce();
  });

  it("cancels without changing a controlled value and accepts subsequent prop changes", async () => {
    const onValueChange = vi.fn();
    const view = render(<Example value="alpha" onValueChange={onValueChange} />);
    const trigger = screen.getByRole("combobox", { name: "Selection" });
    fireEvent.keyDown(trigger, { key: "Enter" });
    const alpha = await screen.findByRole("option", { name: "Alpha" });
    fireEvent.keyDown(alpha, { key: "Escape" });

    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(onValueChange).not.toHaveBeenCalled();
    expect(trigger.textContent).toContain("Alpha");
    await waitFor(() => expect(document.activeElement).toBe(trigger));

    view.rerender(<Example value="gamma" onValueChange={onValueChange} />);
    expect(trigger.textContent).toContain("Gamma");
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it("shows the placeholder when the parent clears a controlled value", () => {
    const onValueChange = vi.fn();
    const view = render(<Example value="alpha" onValueChange={onValueChange} />);
    const trigger = screen.getByRole("combobox", { name: "Selection" });
    view.rerender(<Example value="" onValueChange={onValueChange} />);

    expect(trigger.textContent).toContain("Choose an option");
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it.each(["object", "callback", "callback cleanup"] as const)(
    "forwards Select props and %s refs through the portaled content",
    async (kind) => {
      const triggerRef = createRef<HTMLButtonElement>();
      const contentRef = createRef<HTMLDivElement>();
      const itemRef = createRef<HTMLDivElement>();
      const onRefCleanup = vi.fn();
      const callbackRef = (node: HTMLDivElement | null) => {
        contentRef.current = node;
        if (kind === "callback cleanup" && node) {
          return () => {
            onRefCleanup();
            contentRef.current = null;
          };
        }
      };
      const view = render(
        <Select defaultValue="alpha">
          <SelectTrigger ref={triggerRef} aria-label="Ref selection">
            <SelectValue />
          </SelectTrigger>
          <SelectContent
            ref={kind === "object" ? contentRef : callbackRef}
            id="selection-options"
          >
            <SelectItem ref={itemRef} value="alpha" id="alpha-option">
              Alpha
            </SelectItem>
          </SelectContent>
        </Select>,
      );
      const trigger = screen.getByRole("combobox", { name: "Ref selection" });
      expect(triggerRef.current).toBe(trigger);
      trigger.focus();
      fireEvent.keyDown(trigger, { key: " " });
      const option = await screen.findByRole("option", { name: "Alpha" });
      const content = screen.getByRole("listbox");
      expect(contentRef.current).toBe(content);
      expect(content.id).toBe("selection-options");
      expect(itemRef.current).toBe(option);
      expect(option.id).toBe("alpha-option");
      await waitFor(() => expect(document.activeElement).toBe(option));
      fireEvent.keyDown(option, { key: "Escape" });

      await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
      await waitFor(() => expect(document.activeElement).toBe(trigger));
      expect(content.isConnected).toBe(false);
      expect(contentRef.current).toBeNull();
      expect(onRefCleanup).toHaveBeenCalledTimes(
        kind === "callback cleanup" ? 1 : 0,
      );

      fireEvent.keyDown(trigger, { key: "Enter" });
      const reopenedContent = await screen.findByRole("listbox");
      expect(contentRef.current).toBe(reopenedContent);
      view.unmount();
      expect(reopenedContent.isConnected).toBe(false);
      expect(triggerRef.current).toBeNull();
      expect(contentRef.current).toBeNull();
      expect(itemRef.current).toBeNull();
      expect(onRefCleanup).toHaveBeenCalledTimes(
        kind === "callback cleanup" ? 2 : 0,
      );
    },
  );

  it("keeps a disabled Select closed", () => {
    const onValueChange = vi.fn();
    render(<Example disabled onValueChange={onValueChange} />);
    const trigger = screen.getByRole("combobox", { name: "Selection" });

    expect(trigger).toHaveProperty("disabled", true);
    fireEvent.click(trigger);
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(onValueChange).not.toHaveBeenCalled();
  });
});
