import { Suspense } from "react";
import { BazaarPage } from "@/components/bazaar/BazaarPage";
import { DesktopOnlyRoute } from "@/components/bazaar/DesktopOnlyRoute";

export default function Page() {
  return (
    <Suspense fallback={null}>
      <DesktopOnlyRoute>
        <BazaarPage />
      </DesktopOnlyRoute>
    </Suspense>
  );
}
