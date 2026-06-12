import type { NextConfig } from "next";

// BACKEND_URL is a server-only env var (not NEXT_PUBLIC_) — it never reaches
// the client bundle. Set it to the Railway backend URL in production.
// NEXT_PUBLIC_API_URL should be set to "/api" so all client fetches go through
// this proxy and the auth cookie stays on the frontend's own domain.
const BACKEND_URL = process.env.BACKEND_URL ?? "";

const nextConfig: NextConfig = {
  async rewrites() {
    if (!BACKEND_URL) return [];
    return [
      { source: "/api/:path*",   destination: `${BACKEND_URL}/:path*` },
      { source: "/hooks/:path*", destination: `${BACKEND_URL}/hooks/:path*` },
      { source: "/run/:path*",   destination: `${BACKEND_URL}/run/:path*` },
    ];
  },
};

export default nextConfig;
