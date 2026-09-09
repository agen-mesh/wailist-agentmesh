"use client";
import { useIsHandheld } from "./useIsHandheld";

// Whether this client is a viewer rather than an editor.
//
// The same answer as useIsHandheld today, and deliberately a separate name.
// "Is this a phone?" is a fact about the hardware; "may this client edit?" is a
// policy about the product, and lib/readonly.ts is where that policy lives --
// including the one authoring-shaped exception, workflow.geofence, which a
// phone IS allowed because choosing a place is done where you are.
//
// Collapsing the two would mean that the day the policy changes (a tablet with
// a keyboard, say) every call site has to be re-read to work out which question
// it was actually asking. Keeping the names apart costs one delegating
// function.
//
// Layout is a third question with a third answer: useIsCompact measures width,
// because how something stacks genuinely does depend on available room.
export function useReadOnly(): boolean {
  return useIsHandheld();
}
