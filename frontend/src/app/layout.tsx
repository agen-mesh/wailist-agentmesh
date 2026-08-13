import type { Metadata } from "next";
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
  },
  twitter: {
    card: "summary_large_image",
    title: "AgentMesh",
    description: "Design, deploy, and monitor AI agent workflows.",
  },
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
      <body style={{ margin: 0, background: "var(--bg)", color: "var(--fg)", minHeight: "100vh" }}>
        {children}
      </body>
    </html>
  );
}
