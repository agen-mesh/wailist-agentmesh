import { Suspense } from "react";
import { GeofenceRouteFromUrl } from "@/components/workflows/GeofenceRouteFromUrl";
import { MOBILE_SHELL_ID } from "../page";

// Mirrors the canvas route one level up: the native shell ships a static
// export and cannot prerender a page per workflow, so that build emits a
// single shell page and the real id arrives as ?id=. Reuses MOBILE_SHELL_ID
// from ../page rather than repeating the literal, so the two routes cannot
// drift apart and leave the mobile build with a shell at one path and nothing
// at the other.
export function generateStaticParams() {
  return process.env.MOBILE_BUILD === "1" ? [{ id: MOBILE_SHELL_ID }] : [];
}

export default async function GeofencePageRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  // Suspense boundary: GeofenceRouteFromUrl calls useSearchParams(), which
  // Next 16 requires to sit under one or `next build` fails.
  return (
    <Suspense fallback={null}>
      <GeofenceRouteFromUrl routeId={id} />
    </Suspense>
  );
}
