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
  return <WorkflowRoute key={id} workflowId={id} />;
}
