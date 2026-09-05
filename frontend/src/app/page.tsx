"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { LandingPage } from "@/components/landing/LandingPage";
import { useAuth } from "@/hooks/useAuth";
import { IS_NATIVE } from "@/lib/nativeAuth";

// `/` is two different things depending on who is asking.
//
// On the web it is the marketing page, and that is right: it is the first thing
// a stranger sees.
//
// In the installed app it was ALSO the marketing page, and that is wrong.
// Someone who has already found, downloaded and installed this does not need to
// be told what it is or invited to join a waitlist -- the landing page's own
// menu offers "Overview", "How it works" and "Waitlist", none of which is a
// thing to open an app onto. The WebView loads index.html at launch, so that is
// simply what they got.
//
// There is no server-side gate to lean on either: middleware.ts protects
// /workflows on the web, but it is Next SERVER middleware and the shell ships
// as a static export, so it never runs on device. The decision has to be made
// here.
export default function Home() {
  const { signedIn, loading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    // `loading` is what useAuth reports until the session is settled, and on
    // native that already waits on authReady -- which NativeBoot resolves once
    // the shell has restored (or failed to restore) the persisted token. So
    // this deliberately does not race the boot: it asks after the answer is
    // known, and a shell that fails to boot resolves to signed-out rather than
    // hanging, which lands the user on /signin.
    if (!IS_NATIVE || loading) return;
    // replace, not push: the marketing page must not sit in the back stack, or
    // Android's back gesture from /signin returns to it.
    router.replace(signedIn ? "/workflows" : "/signin");
  }, [signedIn, loading, router]);

  // IS_NATIVE is a build-time constant, so the web bundle keeps the landing
  // page and drops the branch above; the mobile bundle's index.html contains
  // this and never the marketing page. That is why there is no flash of the
  // wrong screen on launch -- it is not rendered and then replaced, it was
  // never in the bundle.
  if (IS_NATIVE) {
    // Deliberately bare. A spinner here would be the app's first frame and
    // would sit under the redirect for a few hundred milliseconds; the app
    // background alone reads as a launch, which is what it is.
    return <div style={{ minHeight: "100dvh", background: "var(--bg)" }} />;
  }

  return <LandingPage signedIn={signedIn} />;
}
