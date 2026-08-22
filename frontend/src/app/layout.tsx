import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  metadataBase: new URL("https://www.agent-mesh.app"),
  title: "AgentMesh",
  description: "Design, deploy, and monitor AI agent workflows.",
  openGraph: {
    title: "AgentMesh",
    description: "Design, deploy, and monitor AI agent workflows.",
    url: "/",
    siteName: "AgentMesh",
    type: "website",
    // No `images` here on purpose: opengraph-image.png (the file convention)
    // already supplies og:image, and it takes precedence over anything set
    // here -- setting both leaves og:image pointing at the generated route
    // while twitter:image points somewhere else, which is how this file
    // briefly ended up advertising two different preview images.
  },
  twitter: {
    card: "summary_large_image",
    title: "AgentMesh",
    description: "Design, deploy, and monitor AI agent workflows.",
  },
  // The square mark, as distinct from the 1200x630 preview banner above.
  // A listing that renders an avatar wants this shape, not the banner, and
  // until now the only thing on the domain was favicon.ico.
  icons: {
    icon: [{ url: "/logo.png", type: "image/png" }],
    apple: [{ url: "/logo.png" }],
  },
};

// Next injects this by default, but the app is now explicitly responsive, so the
// contract is stated rather than inherited. `maximumScale` is deliberately left
// unset: capping zoom locks out anyone who needs to magnify text.
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable}`}
      style={{ fontFamily: "var(--font-sans)" }}
    >
      <body
        style={{
          margin: 0,
          background: "var(--bg)",
          color: "var(--fg)",
          minHeight: "100dvh",
        }}
      >
        {children}
      </body>
    </html>
  );
}
