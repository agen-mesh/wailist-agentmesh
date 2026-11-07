import { Suspense } from "react";
import { ShareRoute } from "@/components/workflows/ShareOpen";
import { MOBILE_SHELL_ID } from "@/lib/routes";

export function generateStaticParams() {
  return process.env.MOBILE_BUILD === "1" ? [{ key: MOBILE_SHELL_ID }] : [];
}

export default async function SharePage({
  params,
}: {
  params: Promise<{ key: string }>;
}) {
  const { key } = await params;
  return (
    <Suspense fallback={null}>
      <ShareRoute routeKey={key} />
    </Suspense>
  );
}
