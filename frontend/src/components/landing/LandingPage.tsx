"use client";
import { useRef, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Logo, Tag, IconArrow } from "@/components/ui";
import { WAITLIST_COUNT } from "@/lib/data";
import { waitlist } from "@/lib/api";

interface LandingPageProps {
  signedIn: boolean;
}

export function LandingPage({ signedIn }: LandingPageProps) {
  const router = useRouter();
  const scrollRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);

  // Fade video in/out at loop boundaries
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    let rafId: number;
    let cancelled = false;

    const tick = () => {
      if (cancelled || !v) return;
      const t = v.currentTime;
      const d = v.duration || 0;
      if (d > 0) {
        if (t < 0.5) v.style.opacity = String(t / 0.5);
        else if (t > d - 0.5) v.style.opacity = String(Math.max(0, (d - t) / 0.5));
        else v.style.opacity = "1";
      }
      rafId = requestAnimationFrame(tick);
    };

    v.style.opacity = "0";
    v.addEventListener("play", () => { rafId = requestAnimationFrame(tick); });
    v.addEventListener("ended", () => {
      v.style.opacity = "0";
      setTimeout(() => { if (!cancelled) { v.currentTime = 0; v.play().catch(() => {}); } }, 100);
    });
    v.play().catch(() => {});
    return () => { cancelled = true; cancelAnimationFrame(rafId); };
  }, []);

  // IntersectionObserver for scroll-reveal
  useEffect(() => {
    const io = new IntersectionObserver((entries) => {
      entries.forEach((e) => { if (e.isIntersecting) e.target.classList.add("visible"); });
    }, { threshold: 0.12 });
    document.querySelectorAll(".in-view").forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);

  const scrollToId = (id: string) => {
    const c = scrollRef.current;
    if (!c) return;
    const el = c.querySelector(`#${id}`);
    if (!el) return;
    c.scrollTo({ top: Math.max(0, (el as HTMLElement).offsetTop - 16), behavior: "smooth" });
  };

  const openStudio = () => {
    if (signedIn) router.push("/workflows");
    else router.push("/signin");
  };

  return (
    <div style={{ height: "100vh", position: "relative", background: "hsl(260 90% 2%)", overflow: "hidden" }}>
      {/* Background video */}
      <video
        ref={videoRef}
        src="https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260328_065045_c44942da-53c6-4804-b734-f9e07fc22e08.mp4"
        muted playsInline autoPlay loop
        style={{
          position: "fixed", inset: 0, width: "100%", height: "100%",
          objectFit: "cover", opacity: 0, transition: "opacity 0.05s linear",
          zIndex: 0, pointerEvents: "none",
        }}
      />

      {/* Scroll container */}
      <div ref={scrollRef} style={{
        position: "relative", zIndex: 1,
        height: "100vh", overflowY: "auto", overflowX: "hidden",
        scrollBehavior: "smooth",
      }}>
        <HeroSection openStudio={openStudio} signedIn={signedIn} scrollToId={scrollToId} />
        <LandingPillars />
        <LandingFlow />
        <LandingWaitlist />
        <LandingFooter />
      </div>
    </div>
  );
}

// ── Hero ──────────────────────────────────────────────────────────────────
function HeroSection({ openStudio, signedIn, scrollToId }: {
  openStudio: () => void;
  signedIn: boolean;
  scrollToId: (id: string) => void;
}) {
  const router = useRouter();
  return (
    <section style={{ position: "relative", minHeight: "100vh", display: "flex", flexDirection: "column", background: "transparent" }}>
      {/* Readability bloom behind headline */}
      <div style={{
        position: "absolute", top: "50%", left: "50%",
        transform: "translate(-50%, -50%)",
        width: 984, height: 527,
        opacity: 0.88,
        background: "hsl(260 60% 4%)",
        filter: "blur(82px)",
        pointerEvents: "none", zIndex: 1,
      }} />

      <div style={{ position: "relative", zIndex: 10, display: "flex", flexDirection: "column", flex: 1, minHeight: "100vh" }}>
        {/* Nav */}
        <div style={{ position: "relative", zIndex: 11 }}>
          <div style={{ padding: "20px 32px", display: "flex", alignItems: "center", justifyContent: "space-between" }}>
            <Logo size={20} />
            <nav style={{
              display: "flex", alignItems: "center", gap: 4,
              position: "absolute", left: "50%", transform: "translateX(-50%)",
            }}>
              {["Overview", "How it works", "Waitlist"].map((label, i) => (
                <button key={label} onClick={() => scrollToId(["pillars", "flow", "waitlist"][i])}
                  style={{
                    background: "transparent", border: "none", cursor: "pointer",
                    color: "rgba(242, 240, 247, 0.9)", fontSize: 14, fontWeight: 400,
                    fontFamily: "var(--font-sans)", padding: "8px 14px",
                    display: "inline-flex", alignItems: "center", gap: 6,
                    transition: "color .15s",
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.color = "#fff")}
                  onMouseLeave={(e) => (e.currentTarget.style.color = "rgba(242, 240, 247, 0.9)")}
                >{label}</button>
              ))}
            </nav>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              {!signedIn && (
                <button onClick={() => router.push("/signup")} className="liquid-glass"
                  style={{
                    padding: "8px 18px", borderRadius: 999,
                    background: "rgba(255,255,255,0.04)", color: "rgba(242, 240, 247, 0.92)",
                    fontSize: 13, fontWeight: 500, border: "none", cursor: "pointer",
                    fontFamily: "var(--font-sans)",
                  }}>Sign Up</button>
              )}
              <button onClick={openStudio}
                style={{
                  padding: "8px 18px", borderRadius: 999,
                  background: "var(--accent)", color: "var(--accent-fg)",
                  fontSize: 13, fontWeight: 600, border: "none", cursor: "pointer",
                  fontFamily: "var(--font-sans)",
                  display: "inline-flex", alignItems: "center", gap: 6,
                }}>
                Open Studio <IconArrow size={11} />
              </button>
            </div>
          </div>
          <div style={{ height: 1, marginTop: 3, background: "linear-gradient(90deg, transparent, rgba(242, 240, 247, 0.20), transparent)" }} />
        </div>

        {/* Hero content */}
        <div style={{
          flex: 1, display: "flex", alignItems: "center", justifyContent: "center",
          padding: "0 32px", position: "relative",
        }}>
          <div style={{
            position: "relative", zIndex: 12, display: "flex", flexDirection: "column",
            alignItems: "center", textAlign: "center",
          }}>
            <h1 style={{
              margin: 0, fontFamily: "var(--font-sans)", fontWeight: 400,
              fontSize: "clamp(80px, 16vw, 220px)", lineHeight: 1.02,
              letterSpacing: "-0.024em", color: "rgba(242, 240, 247, 0.98)",
            }}>
              Agent<span className="hero-gradient">Mesh</span>
            </h1>
            <p style={{
              margin: 0, marginTop: 9, maxWidth: 460,
              fontSize: 18, lineHeight: 1.55, color: "rgba(242, 240, 247, 0.82)",
              opacity: 0.85, fontFamily: "var(--font-sans)", letterSpacing: "-0.005em",
            }}>
              The visual canvas for autonomous<br />agent networks on Algorand.
            </p>
            <div style={{ display: "flex", gap: 12, marginTop: 25 }}>
              <button onClick={openStudio} className="liquid-glass"
                style={{
                  padding: "24px 29px", borderRadius: 999,
                  background: "rgba(255,255,255,0.04)", color: "#fff",
                  fontSize: 15, fontWeight: 500, border: "none", cursor: "pointer",
                  fontFamily: "var(--font-sans)",
                  display: "inline-flex", alignItems: "center", gap: 8,
                }}>
                Open Studio <IconArrow size={13} />
              </button>
            </div>
          </div>
        </div>

        {/* Logo marquee */}
        <LogoMarquee />
      </div>
    </section>
  );
}

function LogoMarquee() {
  const logos = [
    { letter: "A", name: "Algorand" }, { letter: "x", name: "x402" },
    { letter: "G", name: "GoPlausible" }, { letter: "P", name: "Pera Wallet" },
    { letter: "T", name: "Tavily" }, { letter: "F", name: "Firecrawl" },
    { letter: "N", name: "Neon" }, { letter: "A", name: "AlpacaQuote" },
  ];
  const doubled = [...logos, ...logos];

  return (
    <div style={{ padding: "0 32px 40px", position: "relative", zIndex: 11 }}>
      <div style={{ maxWidth: 1100, margin: "0 auto", display: "flex", alignItems: "center", gap: 48 }}>
        <div style={{ flex: "0 0 auto", color: "rgba(242, 240, 247, 0.5)", fontSize: 13, lineHeight: 1.4, fontFamily: "var(--font-sans)" }}>
          Built on the rails<br />of open agentic commerce
        </div>
        <div className="marquee-mask" style={{ flex: 1, overflow: "hidden" }}>
          <div className="marquee-track">
            {doubled.map((l, i) => (
              <div key={i} style={{ display: "inline-flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
                <span className="liquid-glass" style={{
                  width: 24, height: 24, borderRadius: 8,
                  display: "inline-flex", alignItems: "center", justifyContent: "center",
                  fontFamily: "var(--font-mono)", fontSize: 12, fontWeight: 600,
                  color: "rgba(242, 240, 247, 0.95)",
                }}>{l.letter}</span>
                <span style={{ fontFamily: "var(--font-sans)", fontSize: 16, fontWeight: 600, color: "rgba(242, 240, 247, 0.95)", letterSpacing: "-0.01em" }}>{l.name}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Pillars ───────────────────────────────────────────────────────────────
function LandingPillars() {
  const pillars = [
    { tag: "01", kicker: "build", title: "Design agent workflows visually.", body: "Drag triggers, agents, providers, tools, and actions onto a canvas. Connect them like Lego. The graph IS the execution graph.", glyph: "⬡" },
    { tag: "02", kicker: "fund",  title: "Wallets at deploy, not signup.",   body: "Each agent gets an Ed25519 keypair on Algorand testnet the moment you deploy. Fund manually, watch balances tick live.", glyph: "◎" },
    { tag: "03", kicker: "wire",  title: "x402 paywalled APIs as tools.",    body: "Any HTTP 402-compliant endpoint becomes a tool node. Price-discovered at edge-time, settled per call, accountable per agent.", glyph: "⟡" },
    { tag: "04", kicker: "run",   title: "A2A messages anchored on-chain.",  body: "Inter-agent messages produce verifiable receipts. Replay any run by anchor hash. Audit-friendly by default.", glyph: "◈" },
  ];
  return (
    <section id="pillars" style={{
      padding: "128px 32px", borderTop: "1px solid var(--border)",
      position: "relative", zIndex: 1, background: "rgba(4, 3, 12, 0.55)",
    }}>
      <div style={{ maxWidth: 1280, margin: "0 auto" }}>
        <div className="in-view" style={{ marginBottom: 72 }}>
          <Tag>what is agentmesh</Tag>
          <h2 style={{ margin: "16px 0 0", fontSize: 48, fontWeight: 500, letterSpacing: "-0.028em", maxWidth: 680, fontFamily: "var(--font-sans)", lineHeight: 1.12 }}>
            Visual workflow design meets on-chain agentic commerce.
          </h2>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 20 }}>
          {pillars.map((p, i) => (
            <div key={i} className="in-view" style={{ transitionDelay: `${i * 90}ms` }}>
              <div
                style={{ height: "100%", padding: "36px 36px 40px", background: "rgba(255,255,255,0.035)", border: "1px solid rgba(255,255,255,0.08)", borderRadius: 16, backdropFilter: "blur(8px)", position: "relative", overflow: "hidden", transition: "border-color .2s, box-shadow .2s" }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "rgba(167,140,250,0.28)"; (e.currentTarget as HTMLElement).style.boxShadow = "0 0 40px rgba(167,140,250,0.07)"; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "rgba(255,255,255,0.08)"; (e.currentTarget as HTMLElement).style.boxShadow = "none"; }}
              >
                <div style={{ position: "absolute", top: 0, left: 0, right: 0, height: 1, background: "linear-gradient(90deg, transparent, var(--accent), transparent)", opacity: 0.4 }} />
                <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 28 }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)", padding: "3px 9px", border: "1px solid var(--accent-line)", borderRadius: 999, background: "var(--accent-soft)", letterSpacing: "0.06em" }}>{p.tag}</span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", letterSpacing: "0.08em", textTransform: "uppercase" }}>— {p.kicker}</span>
                </div>
                <h3 style={{ margin: 0, fontSize: 22, fontWeight: 500, letterSpacing: "-0.022em", lineHeight: 1.25 }}>{p.title}</h3>
                <p style={{ margin: "14px 0 0", color: "var(--fg-muted)", fontSize: 14.5, lineHeight: 1.65 }}>{p.body}</p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ── Flow steps ────────────────────────────────────────────────────────────
function LandingFlow() {
  const steps = [
    { k: "01", label: "Drop nodes",  body: "Trigger, agent, provider, tools, action, end.",          glyph: "⊕" },
    { k: "02", label: "Wire ports",  body: "Provider + tools attach to agent's bottom ports.",         glyph: "⟡" },
    { k: "03", label: "Deploy",      body: "Agents get Algorand testnet wallets at this moment.",      glyph: "◎" },
    { k: "04", label: "Fund + run",  body: "Top up agent balances. Watch x402 settlements live.",      glyph: "▶" },
  ];
  return (
    <section id="flow" style={{ borderTop: "1px solid var(--border)", background: "rgba(4, 3, 12, 0.62)", padding: "112px 32px", position: "relative", zIndex: 1 }}>
      <div style={{ maxWidth: 1280, margin: "0 auto" }}>
        <div className="in-view" style={{ marginBottom: 64 }}>
          <Tag>flow</Tag>
          <h2 style={{ margin: "16px 0 0", fontSize: 40, fontWeight: 500, letterSpacing: "-0.025em", fontFamily: "var(--font-sans)", lineHeight: 1.15 }}>
            Zero to running pipeline in four moves.
          </h2>
        </div>
        <div className="in-view" style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 14 }}>
          {steps.map((s, i) => (
            <div key={i} style={{ position: "relative" }}>
              {i < steps.length - 1 && (
                <div style={{ position: "absolute", top: 42, right: -10, zIndex: 2, color: "var(--fg-dim)", fontSize: 16, pointerEvents: "none" }}>›</div>
              )}
              <div
                style={{ height: "100%", padding: "28px 24px 32px", background: "rgba(255,255,255,0.03)", border: "1px solid rgba(255,255,255,0.07)", borderRadius: 14, backdropFilter: "blur(6px)", transition: "border-color .2s, background .2s" }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "rgba(167,140,250,0.22)"; (e.currentTarget as HTMLElement).style.background = "rgba(255,255,255,0.055)"; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "rgba(255,255,255,0.07)"; (e.currentTarget as HTMLElement).style.background = "rgba(255,255,255,0.03)"; }}
              >
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 24 }}>
                  <span style={{ width: 36, height: 36, borderRadius: 10, background: "var(--accent-soft)", border: "1px solid var(--accent-line)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 15, color: "var(--accent)" }}>{s.glyph}</span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", letterSpacing: "0.1em" }}>{s.k}</span>
                </div>
                <div style={{ fontSize: 17, fontWeight: 600, letterSpacing: "-0.018em", marginBottom: 10 }}>{s.label}</div>
                <div style={{ color: "var(--fg-muted)", fontSize: 13, lineHeight: 1.6 }}>{s.body}</div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ── Waitlist ──────────────────────────────────────────────────────────────
function LandingWaitlist() {
  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const email = data.get("email") as string;
    try {
      await waitlist.join(email);
      alert("Thanks — we'll be in touch.");
    } catch {
      alert("Thanks — we'll be in touch.");
    }
  };

  return (
    <section id="waitlist" style={{ borderTop: "1px solid var(--border)", padding: "128px 32px", position: "relative", zIndex: 1, background: "rgba(4, 3, 12, 0.55)" }}>
      <div className="in-view" style={{ maxWidth: 540, margin: "0 auto", textAlign: "center" }}>
        <Tag>early access</Tag>
        <h2 style={{ margin: "20px 0 14px", fontSize: 48, fontWeight: 500, letterSpacing: "-0.028em", fontFamily: "var(--font-sans)", lineHeight: 1.1 }}>Join the waitlist.</h2>
        <p style={{ color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.6, marginBottom: 40 }}>
          {WAITLIST_COUNT}+ teams in queue. Early access opens in cohorts; testnet is free.
        </p>
        <div style={{ padding: "32px 32px 28px", background: "rgba(255,255,255,0.04)", border: "1px solid rgba(255,255,255,0.09)", borderRadius: 18, backdropFilter: "blur(12px)", boxShadow: "0 0 60px rgba(167,140,250,0.07), inset 0 1px 0 rgba(255,255,255,0.06)" }}>
          <form onSubmit={handleSubmit} style={{ display: "flex", gap: 8 }}>
            <input name="email" type="email" required placeholder="you@company.com"
              style={{ height: 46, flex: 1, background: "rgba(255,255,255,0.05)", border: "1px solid rgba(255,255,255,0.1)", borderRadius: "var(--r-2)", color: "var(--fg)", fontFamily: "var(--font-sans)", fontSize: 14, padding: "0 12px", outline: "none" }} />
            <button type="submit"
              style={{ height: 46, padding: "0 22px", fontSize: 13, fontWeight: 600, background: "var(--accent)", color: "var(--accent-fg)", border: "none", borderRadius: "var(--r-2)", cursor: "pointer", fontFamily: "var(--font-sans)", boxShadow: "0 0 20px var(--accent-glow)", whiteSpace: "nowrap" }}>
              Request access
            </button>
          </form>
          <div style={{ display: "flex", justifyContent: "center", gap: 32, marginTop: 24, paddingTop: 20, borderTop: "1px solid rgba(255,255,255,0.06)" }}>
            {[{ v: `${WAITLIST_COUNT}+`, l: "teams in queue" }, { v: "free", l: "testnet" }, { v: "x402", l: "native" }].map((s, i) => (
              <div key={i} style={{ textAlign: "center" }}>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 15, fontWeight: 600, color: "var(--accent)" }}>{s.v}</div>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 3, textTransform: "uppercase", letterSpacing: "0.06em" }}>{s.l}</div>
              </div>
            ))}
          </div>
        </div>
        <div style={{ marginTop: 18, fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>no spam · cohort invites only</div>
      </div>
    </section>
  );
}

// ── Footer ────────────────────────────────────────────────────────────────
function LandingFooter() {
  return (
    <footer style={{ borderTop: "1px solid var(--border)", padding: "32px 32px", position: "relative", zIndex: 1, background: "rgba(4, 3, 12, 0.70)" }}>
      <div style={{ maxWidth: 1280, margin: "0 auto", display: "flex", alignItems: "center", justifyContent: "space-between", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <Logo size={14} />
          <span>· built on Algorand · GoPlausible x402</span>
        </div>
        <div style={{ display: "flex", gap: 16 }}>
          <a href="https://github.com/notlevi911/agentmesh" target="_blank" rel="noreferrer" style={{ color: "inherit" }}>GitHub ↗</a>
          <a href="https://algorand.co/agentic-commerce/x402" target="_blank" rel="noreferrer" style={{ color: "inherit" }}>Algorand ↗</a>
          <a href="https://www.x402.org" target="_blank" rel="noreferrer" style={{ color: "inherit" }}>x402 ↗</a>
        </div>
      </div>
    </footer>
  );
}
