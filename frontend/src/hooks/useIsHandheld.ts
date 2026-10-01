"use client";
import { useSyncExternalStore } from "react";
import {
  HOVER_NONE_QUERY,
  POINTER_COARSE_QUERY,
  isHandheld,
  readDeviceSignals,
} from "@/lib/device";

// Whether this client is a phone or tablet, live.
//
// Keyed on the DEVICE, not the window. A laptop dragged narrow is still a
// laptop; a phone is a phone at any size. See lib/device.ts for the decision
// table and why width plays no part in it.
//
// This machinery used to live inside useReadOnly, which is the policy question
// ("may this client edit?") rather than the device question ("is this a
// phone?"). They have the same answer today and are still not the same idea:
// read-only is a rule that could change, handheld is a fact about the hardware.
// The app shell -- bottom navigation, safe areas, touch sizing -- wants the
// fact. useReadOnly now asks this and keeps its own reasoning.
//
// Layout is a third question with a third answer: useIsCompact still measures
// width, because how something stacks genuinely does depend on available room.

// Created once, not per call: useSyncExternalStore invokes the snapshot getter
// on every render of every component that uses this, and CanvasGraph re-renders
// on every pointer-move during a pan, zoom, or node drag. window.matchMedia()
// allocates a new MediaQueryList each time it is called, so calling it fresh
// per render added avoidable allocation to the canvas's highest-frequency
// interaction. Both queries are fixed for the module's lifetime -- only their
// .matches value changes, which subscribe() below already listens for -- so the
// MediaQueryList objects themselves are safe to cache indefinitely.
let cachedQueries: {
  pointerCoarse: MediaQueryList;
  hoverNone: MediaQueryList;
} | null = null;

function mediaQueries() {
  if (typeof window === "undefined" || !window.matchMedia) return null;
  if (!cachedQueries) {
    cachedQueries = {
      pointerCoarse: window.matchMedia(POINTER_COARSE_QUERY),
      hoverNone: window.matchMedia(HOVER_NONE_QUERY),
    };
  }
  return cachedQueries;
}

function subscribe(onChange: () => void): () => void {
  const mq = mediaQueries();
  if (!mq) return () => {};
  // Both queries are live: pairing a Bluetooth mouse to a tablet, or undocking
  // a convertible, changes the primary pointer mid-session.
  mq.pointerCoarse.addEventListener("change", onChange);
  mq.hoverNone.addEventListener("change", onChange);
  return () => {
    mq.pointerCoarse.removeEventListener("change", onChange);
    mq.hoverNone.removeEventListener("change", onChange);
  };
}

function getSnapshot(): boolean {
  const mq = mediaQueries();
  // device.ts's readDeviceSignals() stays the single source of truth for every
  // signal besides the two cached MediaQueryLists -- a hand-copied second
  // implementation here would drift from it silently the next time either
  // function's signal-reading logic changes.
  return isHandheld(
    readDeviceSignals(
      mq
        ? {
            pointerCoarse: mq.pointerCoarse.matches,
            hoverNone: mq.hoverNone.matches,
          }
        : undefined,
    ),
  );
}

// The server has no device to inspect, so it renders the desktop answer. That
// is both the safe default and the common case, and getSnapshot() returns the
// same desktop-shaped answer when `window` is missing -- so the SSR markup and
// the first client render agree, and React reconciles afterwards on the devices
// that need it.
//
// Anything rendered ONLY on handheld therefore appears after hydration rather
// than in the server HTML. That is the correct trade and not a bug to route
// around: the alternative is markup that does not match, which React discards
// wholesale.
function getServerSnapshot(): boolean {
  return false;
}

export function useIsHandheld(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
