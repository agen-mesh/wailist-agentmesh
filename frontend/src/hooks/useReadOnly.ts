"use client";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { useIsHandheld } from "./useIsHandheld";

// Whether this client is a viewer rather than an editor: the native app, or
// any phone or tablet.
//
// "Is this a phone?" is a fact about the hardware; "may this client edit?" is a
// policy about the product, and lib/readonly.ts is where that policy lives --
// including the one authoring-shaped exception, workflow.geofence, which a
// phone IS allowed because choosing a place is done where you are.
//
// The native app is checked directly rather than left to lib/device.ts, which
// classifies its WebView as a handheld only because Android reports it as one.
// A build running on a tablet with a mouse attached is still the native app,
// and the native app never edits a workflow.
//
// Layout is a third question with a third answer: useIsCompact measures width,
// because how something stacks genuinely does depend on available room.
export function useReadOnly(): boolean {
  // Called unconditionally: `IS_NATIVE || useIsHandheld()` would skip the hook
  // in the native build and break the rules of hooks.
  const handheld = useIsHandheld();
  return IS_NATIVE || handheld;
}
