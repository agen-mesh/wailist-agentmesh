import { useEffect, useRef } from "react";
import { useCloseOnBack } from "./useCloseOnBack";

// Shared modal behavior: close on Escape, lock body scroll while active so
// whatever's behind the modal can't scroll, and keep Tab inside the dialog.
// Previously hand-rolled separately in CheckoutModal and AddToWorkflowDialog
// (the second copy's own comment said "matching CheckoutModal") -- a future fix
// (e.g. nested-dialog scroll-unlock ordering) now only has to be applied here
// once.
//
// `active` defaults to true for a dialog that's only ever mounted while open
// (e.g. AddToWorkflowDialog, rendered conditionally by its parent); pass it
// explicitly for a component that stays mounted and toggles visibility
// itself (e.g. CheckoutModal's `open` prop).
//
// Returns a ref the caller puts on its panel -- the element carrying
// role="dialog". See the focus effect below for why the hook has to be told
// rather than work it out.
export function useModalDismissal<T extends HTMLElement = HTMLDivElement>(
  onClose: () => void,
  active = true,
) {
  const dialogRef = useRef<T | null>(null);

  // The Android Back gesture closes the dialog instead of leaving the page
  // under it. The function useCloseOnBack returns is not needed here: every
  // dialog using this hook closes through its own onClose, and the hook takes
  // its history entry off when the dialog goes away.
  useCloseOnBack(onClose, active);

  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active, onClose]);

  useEffect(() => {
    if (!active) return;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = "";
    };
  }, [active]);

  // Keep Tab inside the dialog, and give focus back when it closes.
  //
  // Every dialog here already declares aria-modal, which PROMISES assistive
  // technology that the rest of the page is inert -- and nothing enforced it.
  // Tab walked straight out into the page behind, which for a screen-reader
  // or keyboard-only user meant operating controls they had been told were
  // not there, with no way back but Escape.
  //
  // In the hook rather than in each dialog, for the reason this file already
  // gives: four dialogs use it, and a trap implemented four times is a trap
  // implemented wrong three times.
  //
  // The panel arrives as a ref because asking the document for it does not
  // work. RunSheet (which traps focus itself), NotificationsSheet and the
  // Topbar menu all render aria-modal without going through this hook, so
  // "the first [aria-modal='true'] in the document" can be somebody else's
  // element -- and trapping focus in the wrong one is worse than not trapping
  // at all. An earlier version handled that by giving up whenever more than
  // one was open, which was safe but left the keyboard user with exactly the
  // behaviour this exists to remove. The caller knows which element is its
  // own; nothing else does.
  useEffect(() => {
    if (!active) return;
    const dialog = dialogRef.current;
    // Where focus was before the dialog opened -- the row menu item, the
    // toolbar button. Putting it back is what lets somebody carry on down the
    // list instead of being dropped at the top of the document.
    const previous = document.activeElement as HTMLElement | null;

    // Focus the dialog itself rather than its first control: a dialog whose
    // first control is destructive should not open with that control armed
    // under the keyboard.
    if (dialog) {
      dialog.setAttribute("tabindex", "-1");
      dialog.focus({ preventScroll: true });
    }

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || !dialog) return;
      // Queried per keypress, not once: these dialogs swap their own contents
      // (Import replaces its textarea with a preview card), so a list taken
      // on open goes stale while the dialog is still open.
      const focusable = dialog.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const on = document.activeElement;

      if (e.shiftKey && (on === first || on === dialog)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && on === last) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      // Only when nothing else has claimed focus in the meantime.
      //
      // "Still inside the dialog" is the obvious test and it is the wrong one:
      // three of the four dialogs close by being UNMOUNTED, and React runs this
      // cleanup after it has already taken the panel out of the document --
      // at which point focus has fallen to <body> and the panel contains
      // nothing. Written that way the restore silently never happened for
      // them, which is the whole point of keeping `previous` around.
      //
      // `previous.isConnected` is what keeps the original intent: a dialog
      // that closed BECAUSE the app navigated leaves its opener detached, so
      // focus is not yanked back to a button that no longer exists.
      const on = document.activeElement;
      const unclaimed = !on || on === document.body || dialog?.contains(on);
      if (previous?.isConnected && unclaimed) {
        previous.focus({ preventScroll: true });
      }
    };
  }, [active]);

  return dialogRef;
}
