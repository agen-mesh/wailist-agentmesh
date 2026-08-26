import { Suspense } from "react";
import { WorkflowRoute } from "@/components/workflows/WorkflowRoute";

export default async function CanvasPageRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  // key ties component identity to the workflow id: switching workflows
  // remounts WorkflowRoute so all editor/console state resets to initial
  // values instead of carrying over from whatever was previously open.
  // Suspense boundary: WorkflowRoute renders CanvasPage, which calls
  // useSearchParams() for the Bazaar `?add=` handoff -- Next 16 requires a
  // Suspense boundary somewhere above that call or `next build` fails.
  return (
    <Suspense fallback={null}>
      <WorkflowRoute key={id} workflowId={id} />
    </Suspense>
  );
}
