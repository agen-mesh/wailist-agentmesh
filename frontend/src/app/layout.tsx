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
  title: "AgentMesh — Visual canvas for autonomous agent networks",
  description:
    "Give your AI agents a wallet. Let them pay their own way. Build autonomous AI agent workflows with on-chain micropayments on Algorand.",
  // OpenGraph tags — the GoPlausible facilitator crawls FRONTEND_URL to extract
  // branding for the Bazaar discovery catalog and x402 Global Challenge
  // leaderboard. og:site_name becomes the merchant label, og:image the logo.
  openGraph: {
    siteName: "AgentMesh",
    title: "AgentMesh",
    description:
      "No-code platform for building autonomous AI agent workflows with real-time Algorand micropayments. Agents pay for the APIs they use, on-chain, without API keys.",
    type: "website",
    url: "https://www.agent-mesh.app",
    images: [
      {
        url: "https://www.agent-mesh.app/og-image.png",
        width: 1024,
        height: 1024,
        alt: "AgentMesh — AI agent workflow platform with Algorand payments",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "AgentMesh — Give your AI agents a wallet",
    description:
      "No-code platform for autonomous AI agent workflows with on-chain micropayments on Algorand.",
    images: ["https://www.agent-mesh.app/og-image.png"],
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
