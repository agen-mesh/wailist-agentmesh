import { CanvasPage } from "@/components/canvas/CanvasPage";

export default async function CanvasPageRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  // key ties component identity to the workflow id: switching workflows
  // remounts CanvasPage so all editor state resets to initial values.
  return <CanvasPage key={id} workflowId={id} />;
}
