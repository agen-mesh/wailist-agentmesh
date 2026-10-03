import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useModalDismissal } from "./useModalDismissal";

// The Android Back half has its own file (useCloseOnBack.test.tsx) and pulls in
// the device mocks with it. What is under test here is the focus trap.
vi.mock("./useCloseOnBack", () => ({ useCloseOnBack: () => () => {} }));

afterEach(cleanup);

// Something else in the app already showing an aria-modal element, rendered
// BEFORE the dialog under test so it is the first one a document query would
// find. RunSheet, NotificationsSheet and the Topbar menu are the real ones.
function Harness({ onClose = () => {} }: { onClose?: () => void }) {
  const dialogRef = useModalDismissal(onClose);
  return (
    <>
      <div role="dialog" aria-modal="true" aria-label="Someone else's sheet">
        <button type="button">Outsider</button>
      </div>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-label="Mine">
        <button type="button">First</button>
        <button type="button">Last</button>
      </div>
    </>
  );
}

describe("useModalDismissal focus trap", () => {
  it("traps the caller's own panel, not the first aria-modal on the page", () => {
    // The regression this file exists for. The hook used to ask the document
    // for "[aria-modal='true']", which here answers with somebody else's sheet
    // first -- so an earlier version gave up whenever more than one was open
    // and Tab walked straight out of the dialog. The caller now says which
    // element is its own.
    render(<Harness />);

    screen.getByText("Last").focus();
    fireEvent.keyDown(document, { key: "Tab" });

    expect(document.activeElement).toBe(screen.getByText("First"));
  });

  it("wraps backwards from the first control to the last", () => {
    render(<Harness />);

    screen.getByText("First").focus();
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });

    expect(document.activeElement).toBe(screen.getByText("Last"));
  });

  it("opens with the panel focused rather than its first control", () => {
    // A dialog whose first control is destructive should not open with that
    // control armed under the keyboard.
    render(<Harness />);

    expect(document.activeElement).toBe(
      screen.getByRole("dialog", { name: "Mine" }),
    );
  });

  it("gives focus back to whatever opened it", () => {
    // By unmounting, which is how three of the four dialogs close -- their
    // parent stops rendering them. The restore used to test whether focus was
    // still inside the panel, and by the time React runs the cleanup the panel
    // is already out of the document, so for those three it never fired.
    const opener = document.createElement("button");
    document.body.appendChild(opener);
    opener.focus();

    const view = render(<Harness />);
    expect(document.activeElement).not.toBe(opener);

    view.unmount();
    expect(document.activeElement).toBe(opener);

    opener.remove();
  });
});
