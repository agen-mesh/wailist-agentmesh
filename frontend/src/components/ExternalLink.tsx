"use client";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { openExternal } from "@/lib/openExternal";

// A link to a page outside AgentMesh, such as a block explorer. It stays an
// ordinary new-tab link on the web. In the Android app it opens an in-app
// browser tab that closes back to this screen, where a plain target="_blank"
// link would leave the app for the system browser.
export function ExternalLink({
  href,
  style,
  children,
}: {
  href?: string;
  style?: React.CSSProperties;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      style={style}
      onClick={(e) => {
        if (!IS_NATIVE || !href) return;
        e.preventDefault();
        void openExternal(href);
      }}
    >
      {children}
    </a>
  );
}
