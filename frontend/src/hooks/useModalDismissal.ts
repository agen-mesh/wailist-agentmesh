import { useEffect } from "react";
import { useCloseOnBack } from "./useCloseOnBack";

// Shared modal behavior: close on Escape, and lock body scroll while active
// so whatever's behind the modal can't scroll. Previously hand-rolled
// separately in CheckoutModal and AddToWorkflowDialog (the second copy's own
// comment said "matching CheckoutModal") — a future fix (e.g. nested-dialog
// scroll-unlock ordering) now only has to be applied here once.
//
// `active` defaults to true for a dialog that's only ever mounted while open
// (e.g. AddToWorkflowDialog, rendered conditionally by its parent); pass it
// explicitly for a component that stays mounted and toggles visibility
// itself (e.g. CheckoutModal's `open` prop).
export function useModalDismissal(onClose: () => void, active = true) {
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
}
