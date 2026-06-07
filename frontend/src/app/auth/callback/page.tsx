"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";

// Landing point for backend OAuth: the Go server redirects here with ?token=<jwt>.
// We persist the token the same way email/password login does, then move on.
export default function AuthCallbackPage() {
  const router = useRouter();

  useEffect(() => {
    const token = new URLSearchParams(window.location.search).get("token");
    if (token) {
      localStorage.setItem("agentmesh_signed_in", "1");
      localStorage.setItem("agentmesh_token", token);
      router.replace("/workflows");
    } else {
      router.replace("/signin?error=oauth");
    }
  }, [router]);

  return (
    <div style={{
      height: "100vh", display: "flex", alignItems: "center", justifyContent: "center",
      background: "var(--bg)", color: "var(--fg-muted)", fontFamily: "var(--font-mono)", fontSize: 13,
    }}>
      Signing you in…
    </div>
  );
}
