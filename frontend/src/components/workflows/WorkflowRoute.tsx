"use client";
import { useSyncExternalStore } from "react";
import { CanvasPage } from "@/components/canvas/CanvasPage";
import { useIsHandheld } from "@/hooks/useIsHandheld";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { WorkflowSummary } from "./WorkflowSummary";

// Every workflow id opens the canvas. Partner consoles (Tendril, Prism) used
// to be dispatched from here too -- one hidden row per user per partner,
// matched by id against tendril/prism.consoleWorkflowIdIfExists() on every
// single workflow-page visit, canvas or not, just to answer "is this one of
// the two console ids". They now live at their own routes (/bazaar/tendril,
// /bazaar/prism): neither console page ever read a workflow id in the first
// place (they drive off /tendril/* and /prism/* directly), so the id-match
// dispatch was pure per-visit overhead -- two network calls and a loading
// flicker -- for a question a route segment now answers for free.
//
// On a phone or tablet the canvas is replaced by WorkflowSummary: run status,
// run history and Run/Stop, which is what a handheld is used for. The native
// app is always handheld, so it never ships the choice to the device.
//
// On the web the device is only known in the browser, and the server renders
// the desktop answer. Rendering CanvasPage until hydration would mount the
// whole editor on a phone for one frame and fire its requests, so nothing
// workflow-specific renders until the client has answered.

const subscribeNever = () => () => {};

// False during the server render and hydration, true afterwards.
function useHasMounted(): boolean {
  return useSyncExternalStore(
    subscribeNever,
    () => true,
    () => false,
  );
}

export function WorkflowRoute({ workflowId }: { workflowId: string }) {
  const handheld = useIsHandheld();
  const mounted = useHasMounted();

  if (IS_NATIVE || (mounted && handheld)) {
    return <WorkflowSummary key={workflowId} workflowId={workflowId} />;
  }
  if (!mounted) {
    return (
      <div
        className="am-viewport"
        aria-busy="true"
        style={{ height: "100dvh", background: "var(--bg)" }}
      />
    );
  }
  return <CanvasPage key={workflowId} workflowId={workflowId} />;
}
