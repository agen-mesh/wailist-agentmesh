import { SharedLinksPage } from "@/components/share/SharedLinksPage";

// Under /account rather than at a top level of its own, for two reasons.
//
// middleware.ts already protects "/account/:path*", so this page sits behind
// sign-in with no change to the matcher -- a new top-level prefix would have
// to be added there by hand, and that is exactly the omission that silently
// publishes a private screen.
//
// It also keeps a route named for sharing well away from "/s/", which is the
// PUBLIC end of a share link and deliberately outside the protected list.
export default function SharedLinksRoute() {
  return <SharedLinksPage />;
}
