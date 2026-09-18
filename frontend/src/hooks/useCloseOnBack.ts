"use client";
import { useCallback, useEffect, useRef } from "react";

// Lets the system Back gesture close an open sheet instead of leaving the page
// underneath it.
//
// The Android app has no backButton listener on purpose (see the comment in
// AndroidManifest.xml): Capacitor steps back through the WebView's history.
// So the sheet adds one history entry for itself when it opens, and Back takes
// that entry off rather than the page. The URL does not change, and the entry
// carries the current history state so the router still recognises it.
//
// Returns the function to call when the sheet closes itself (a Close button,
// Escape, a tap outside): it removes the entry first, so a later Back is not
// spent on a sheet that is already gone.

const KEY = "__amSheet";

// `active` is for a sheet that stays MOUNTED and toggles a flag instead (the
// top bar menu, CheckoutModal): the entry is added when it opens and taken off
// when it closes. A sheet its parent renders only while open can leave it out.
export function useCloseOnBack(onClose: () => void, active = true): () => void {
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });
  // Whether our entry is still on the stack.
  const pushed = useRef(false);
  // Set when a cleanup has scheduled the entry for removal, and cleared if the
  // hook re-activates before that runs -- which is exactly what React does in
  // development, where it invokes an effect, its cleanup, then the effect
  // again. Without this, that second pass would leave the sheet with no entry
  // of its own and Back would do nothing.
  const popScheduled = useRef(false);

  useEffect(() => {
    if (!active) return;
    popScheduled.current = false;
    const state = window.history.state as Record<string, unknown> | null;
    // Skipped when the entry is already there, which is the case when an
    // effect runs twice in development.
    if (!state?.[KEY]) {
      window.history.pushState({ ...(state ?? {}), [KEY]: true }, "");
    }
    pushed.current = true;

    const onPopState = () => {
      if (!pushed.current) return;
      pushed.current = false;
      onCloseRef.current();
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("popstate", onPopState);
      // The sheet closed some other way: a control that calls its own onClose
      // rather than the function returned below, or a parent that simply
      // stopped rendering it. Our entry has to come off either way, or the
      // next Back is spent on a sheet that is already gone.
      if (!pushed.current) return;
      popScheduled.current = true;
      window.setTimeout(() => {
        if (!popScheduled.current) return;
        popScheduled.current = false;
        pushed.current = false;
        window.history.back();
      }, 0);
    };
  }, [active]);

  return useCallback(() => {
    if (pushed.current) {
      pushed.current = false;
      window.history.back();
    }
    onCloseRef.current();
  }, []);
}
