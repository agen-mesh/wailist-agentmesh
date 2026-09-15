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

export function useCloseOnBack(onClose: () => void): () => void {
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });
  // Whether our entry is still on the stack.
  const pushed = useRef(false);

  useEffect(() => {
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
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  return useCallback(() => {
    if (pushed.current) {
      pushed.current = false;
      window.history.back();
    }
    onCloseRef.current();
  }, []);
}
