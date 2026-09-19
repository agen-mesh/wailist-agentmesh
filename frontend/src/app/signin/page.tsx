import { Suspense } from "react";
import { AuthPage } from "@/components/auth/AuthPage";

export default function SignInPage() {
  // AuthPage reads ?error= with useSearchParams, which a static export only
  // builds under a Suspense boundary.
  return (
    <Suspense fallback={null}>
      <AuthPage initialMode="signin" />
    </Suspense>
  );
}
