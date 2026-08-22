"use client";
import { useEffect, useState } from "react";
import { CanvasPage } from "@/components/canvas/CanvasPage";
import { TendrilConsolePage } from "@/components/tendril/TendrilConsolePage";
import { tendril } from "@/lib/tendril";

// Most workflow ids open the normal canvas. The one id that is this user's
// Tendril console (backend: GetOrCreateSystemWorkflow, one hidden row per
// user, matched here by id -- never by name, since the backend's real row
// name has no fixed relationship to any frontend constant) opens the
// console instead: renting real hardware is a lookup-and-press-buttons
// task, not something that benefits from a node graph, so that row never
// shows the editor -- there's nothing on its canvas to show in the first
// place.
//
// Uses consoleWorkflowIdIfExists(), NOT console() -- the latter creates the
// console row on first call, which here would mean every workflow-page
// visit silently minting a hidden "Tendril Console" row for users who have
// never touched Tendril at all, just from opening one of their own,
// unrelated workflows. A "not found yet" answer trivially resolves to
// "this isn't the console" without ever needing to create it.
export function WorkflowRoute({ workflowId }: { workflowId: string }) {
  const [isTendrilConsole, setIsTendrilConsole] = useState<boolean | null>(
    () => (workflowId === "new" ? false : null),
  );

  useEffect(() => {
    if (workflowId === "new") return;
    let stale = false;
    tendril
      .consoleWorkflowIdIfExists()
      .then((consoleWorkflowId) => {
        if (!stale) setIsTendrilConsole(workflowId === consoleWorkflowId);
      })
      .catch(() => {
        if (!stale) setIsTendrilConsole(false);
      });
    return () => {
      stale = true;
    };
  }, [workflowId]);

  if (isTendrilConsole === null) {
    return (
      <div
        style={{
          height: "100dvh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "var(--bg)",
          color: "var(--fg-dim)",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
        }}
      >
        loading…
      </div>
    );
  }
  if (isTendrilConsole) return <TendrilConsolePage />;
  return <CanvasPage key={workflowId} workflowId={workflowId} />;
}
