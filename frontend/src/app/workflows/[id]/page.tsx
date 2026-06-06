import { CanvasPage } from "@/components/canvas/CanvasPage";

export default async function CanvasPageRoute({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <CanvasPage workflowId={id} />;
}
