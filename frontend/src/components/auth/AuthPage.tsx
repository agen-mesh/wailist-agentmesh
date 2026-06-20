"use client";
import { useState, useEffect } from "react";
import { useMobile } from "@/hooks/useMobile";
import { useRouter } from "next/navigation";
import { Logo, IconArrow, Pill, Tag } from "@/components/ui";
import { useAuth } from "@/hooks/useAuth";
import { auth } from "@/lib/api";

const OAUTH_ERRORS: Record<string, string> = {
  invalid_state: "Login session expired. Please try again.",
  no_code: "Provider did not return an authorization code.",
  token_exchange: "Could not complete sign in with the provider.",
  no_email: "Could not read a verified email from the provider.",
  account_exists: "An account with this email already exists. Sign in with your email and password.",
  user_upsert: "Could not create your account. Please try again.",
  token_issue: "Could not issue a session. Please try again.",
  internal: "Something went wrong. Please try again.",
  oauth: "Sign in was cancelled or failed.",
};

type Mode = "signin" | "signup";

interface AuthPageProps {
  initialMode?: Mode;
}

export function AuthPage({ initialMode = "signin" }: AuthPageProps) {
  const router = useRouter();
  const isMobile = useMobile();
  const { signIn, signUp } = useAuth();
  const [mode, setMode] = useState<Mode>(initialMode);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [org, setOrg] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // Surface OAuth failures the backend redirected back with (?error=...).
  useEffect(() => {
    const code = new URLSearchParams(window.location.search).get("error");
    if (code) {
      setError(OAUTH_ERRORS[code] ?? "Something went wrong. Please try again.");
      window.history.replaceState({}, "", window.location.pathname);
    }
  }, []);

  const handleOAuth = (provider: "github" | "google") => {
    const url = auth.oauthURL(provider);
    if (!url) {
      setError("Social sign in is not configured.");
      return;
    }
    window.location.href = url;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      if (mode === "signin") {
        await signIn(email, password);
      } else {
        await signUp(email, password, org);
      }
      router.push("/workflows");
    } catch {
      setError("Something went wrong. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ display: "grid", gridTemplateColumns: isMobile ? "1fr" : "1fr 1fr", height: "100vh", overflow: "hidden", background: "var(--bg)" }}>
      {/* Left — form */}
      <div style={{ padding: isMobile ? "32px 24px" : "40px 56px", display: "flex", flexDirection: "column", background: "var(--bg)", overflow: "auto" }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0 }}>
            <Logo size={18} />
          </button>
          <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>v0.4 · testnet</div>
        </div>

        <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", marginTop: 16, marginBottom: 16 }}>
          <div style={{ width: "100%", maxWidth: 360 }} className="reveal">
            <h1 style={{ margin: 0, fontSize: 32, fontWeight: 500, letterSpacing: "-0.025em" }}>
              {mode === "signin" ? "Welcome back." : "Create your account."}
            </h1>
            <p style={{ marginTop: 8, color: "var(--fg-muted)", fontSize: 14 }}>
              {mode === "signin" ? "Sign in to your AgentMesh workspace." : "Free testnet access. Mainnet by invite."}
            </p>

            <form onSubmit={handleSubmit} style={{ marginTop: 32, display: "flex", flexDirection: "column", gap: 12 }}>
              {mode === "signup" && (
                <FormField label="Organization">
                  <input style={inputStyle} value={org} onChange={(e) => setOrg(e.target.value)} placeholder="Acme Capital" />
                </FormField>
              )}
              <FormField label="Work email">
                <input style={inputStyle} type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@company.com" />
              </FormField>
              <FormField
                label="Password"
                hint={mode === "signin"
                  ? <span style={{ color: "var(--fg-muted)", cursor: "pointer" }}>Forgot?</span>
                  : <span style={{ color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 10 }}>min 12 chars</span>}
              >
                <input style={inputStyle} type="password" required value={password} onChange={(e) => setPassword(e.target.value)} placeholder="•••••••••••" />
              </FormField>

              {error && <div style={{ color: "var(--danger)", fontSize: 12, fontFamily: "var(--font-mono)" }}>{error}</div>}

              <button type="submit" disabled={loading}
                style={{
                  height: 42, marginTop: 12, display: "flex", alignItems: "center", justifyContent: "center", gap: 6,
                  background: "var(--accent)", color: "var(--accent-fg)", border: "none", borderRadius: "var(--r-2)",
                  fontSize: 14, fontWeight: 600, fontFamily: "var(--font-sans)", cursor: loading ? "not-allowed" : "pointer",
                  opacity: loading ? 0.7 : 1,
                }}>
                {loading ? "Please wait…" : (mode === "signin" ? "Sign in" : "Create account")}
                {!loading && <IconArrow size={13} />}
              </button>

              <div style={{ display: "flex", alignItems: "center", gap: 10, margin: "8px 0", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
                <div style={{ flex: 1, height: 1, background: "var(--border)" }} />
                <span>or</span>
                <div style={{ flex: 1, height: 1, background: "var(--border)" }} />
              </div>

              <button type="button" onClick={() => handleOAuth("github")} style={ghostBtnStyle}>
                <span style={{ fontFamily: "var(--font-mono)" }}>⌘</span> Continue with GitHub
              </button>
              <button type="button" onClick={() => handleOAuth("google")} style={ghostBtnStyle}>
                <span style={{ color: "var(--accent)" }}>⬡</span> Continue with Google
              </button>
            </form>

            <div style={{ marginTop: 32, fontSize: 13, color: "var(--fg-muted)", textAlign: "center" }}>
              {mode === "signin" ? (
                <>New here? <button onClick={() => setMode("signup")} style={{ background: "transparent", border: "none", color: "var(--accent)", cursor: "pointer", fontSize: 13, fontFamily: "var(--font-sans)", padding: 0 }}>Create an account</button></>
              ) : (
                <>Have an account? <button onClick={() => setMode("signin")} style={{ background: "transparent", border: "none", color: "var(--accent)", cursor: "pointer", fontSize: 13, fontFamily: "var(--font-sans)", padding: 0 }}>Sign in</button></>
              )}
            </div>
          </div>
        </div>

        <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", display: "flex", justifyContent: "space-between" }}>
          <span>· enterprise SSO available</span>
          <span>SOC 2 Type I · in progress</span>
        </div>
      </div>

      {/* Right — visual (desktop only) */}
      {!isMobile && (
        <div style={{
          background: "var(--bg-elev-1)", borderLeft: "1px solid var(--border)",
          position: "relative", overflow: "hidden",
          backgroundImage: "radial-gradient(var(--border-strong) 1px, transparent 1px)",
          backgroundSize: "20px 20px",
        }}>
          <AuthVisual />
        </div>
      )}
    </div>
  );
}

function AuthVisual() {
  const cards = [
    { kicker: "ai agent",  name: "Support Triage", sub: "Gemini · 2 tools", tone: "accent",   delay: "0s" },
    { kicker: "x402 tool", name: "AlpacaQuote",    sub: "0.001 ALGO / quote", tone: "magenta", delay: "0.15s" },
    { kicker: "provider",  name: "Google Gemini",  sub: "1.5 Pro", tone: "default",  delay: "0.30s" },
  ];

  return (
    <div style={{ position: "absolute", inset: 0, padding: 48, display: "flex", flexDirection: "column", justifyContent: "space-between" }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 14, alignItems: "flex-end" }}>
        {cards.map((c, i) => {
          const accent = c.tone === "magenta" ? "#E879F9" : c.tone === "accent" ? "var(--accent)" : "var(--fg-muted)";
          const borderColor = c.tone === "default" ? "var(--border)" : `color-mix(in oklab, ${accent} 30%, var(--border))`;
          return (
            <div key={i} style={{
              width: 280,
              background: "var(--bg-elev-2)",
              border: `1px solid ${borderColor}`,
              borderRadius: "var(--r-3)",
              padding: "12px 14px",
              boxShadow: "0 8px 32px rgba(0,0,0,0.4)",
              opacity: 0,
              animation: `fade-up 0.6s var(--ease) ${c.delay} forwards, float-y 4s ease-in-out ${c.delay} infinite`,
            }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 9.5, color: accent, textTransform: "uppercase", letterSpacing: "0.08em" }}>{c.kicker}</div>
              <div style={{ marginTop: 4, fontSize: 14, fontWeight: 500 }}>{c.name}</div>
              <div style={{ marginTop: 2, fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>{c.sub}</div>
            </div>
          );
        })}
      </div>

      <div className="reveal reveal-delay-3" style={{ maxWidth: 480 }}>
        <Tag>build · fund · wire · run</Tag>
        <div style={{ marginTop: 14, fontSize: 30, fontWeight: 500, letterSpacing: "-0.025em", lineHeight: 1.15 }}>
          Wallets come at deploy.
          <span style={{ color: "var(--accent)" }}> Spend is accountable, per agent.</span>
        </div>
        <div style={{ marginTop: 16, fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
          x402 micropayments · A2A receipts on-chain
        </div>
      </div>
    </div>
  );
}

function FormField({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)", textTransform: "uppercase", letterSpacing: "0.08em" }}>
        <span>{label}</span>
        {hint && <span style={{ textTransform: "none", letterSpacing: 0 }}>{hint}</span>}
      </div>
      {children}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  height: 38, padding: "0 12px", width: "100%",
  background: "var(--bg-elev-1)", border: "1px solid var(--border)",
  borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13,
  fontFamily: "var(--font-sans)", outline: "none",
};

const ghostBtnStyle: React.CSSProperties = {
  height: 40, display: "flex", alignItems: "center", justifyContent: "center", gap: 8,
  background: "var(--bg-elev-1)", border: "1px solid var(--border)",
  borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontWeight: 500,
  fontFamily: "var(--font-sans)", cursor: "pointer",
};
