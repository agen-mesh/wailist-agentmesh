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
      {/* Readability bloom — shifted left to cover copy area */}
      <div style={{
        position: "absolute", top: "48%", left: "28%",
        transform: "translate(-50%, -50%)",
        width: 900, height: 700,
        opacity: 0.92,
        background: "hsl(260 60% 3%)",
        filter: "blur(100px)",
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

        {/* Hero body — split layout */}
        <div style={{
          flex: 1, display: "flex", alignItems: "center",
          padding: "0 48px 0 56px",
          maxWidth: 1340, margin: "0 auto", width: "100%",
          gap: 56,
        }}>
          {/* Left: copy */}
          <div style={{ flex: "0 0 auto", maxWidth: 468, position: "relative", zIndex: 12 }}>
            {/* Tag chip */}
            <div style={{
              display: "inline-flex", alignItems: "center", gap: 8,
              padding: "5px 13px 5px 8px", borderRadius: 999,
              border: "1px solid rgba(167,140,250,0.35)", background: "rgba(167,140,250,0.10)",
              marginBottom: 26,
            }}>
              <span style={{ width: 6, height: 6, borderRadius: "50%", background: "#A78BFA", display: "block", flexShrink: 0, boxShadow: "0 0 6px #A78BFA" }} />
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "#A78BFA", letterSpacing: "0.06em", textTransform: "uppercase" }}>No-code AI orchestration</span>
            </div>

            {/* Headline */}
            <h1 style={{
              margin: 0, fontFamily: "var(--font-sans)", fontWeight: 500,
              fontSize: "clamp(34px, 3.6vw, 52px)", lineHeight: 1.07,
              letterSpacing: "-0.030em", color: "rgba(242, 240, 247, 0.98)",
            }}>
              Your AI agents shouldn&apos;t need a backend engineer.
            </h1>

            {/* Sub */}
            <p style={{
              margin: "20px 0 0", maxWidth: 420,
              fontSize: 16.5, lineHeight: 1.65, color: "rgba(242, 240, 247, 0.60)",
              fontFamily: "var(--font-sans)", letterSpacing: "-0.005em",
            }}>
              Design autonomous pipelines on a visual canvas. Every agent gets a crypto wallet, discovers x402-paywalled tools, and runs end-to-end — no infrastructure, no glue code.
            </p>

            {/* CTAs */}
            <div style={{ display: "flex", gap: 10, marginTop: 34 }}>
              <button onClick={openStudio}
                style={{
                  padding: "13px 24px", borderRadius: 999,
                  background: "var(--accent)", color: "var(--accent-fg)",
                  fontSize: 14, fontWeight: 600, border: "none", cursor: "pointer",
                  fontFamily: "var(--font-sans)",
                  display: "inline-flex", alignItems: "center", gap: 8,
                  boxShadow: "0 0 32px rgba(167,140,250,0.40)",
                  transition: "box-shadow .2s, transform .15s",
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.boxShadow = "0 0 48px rgba(167,140,250,0.60)"; (e.currentTarget as HTMLElement).style.transform = "translateY(-1px)"; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.boxShadow = "0 0 32px rgba(167,140,250,0.40)"; (e.currentTarget as HTMLElement).style.transform = "translateY(0)"; }}
              >
                Start building free <IconArrow size={12} />
              </button>
              <button onClick={() => scrollToId("flow")} className="liquid-glass"
                style={{
                  padding: "13px 24px", borderRadius: 999,
                  background: "rgba(255,255,255,0.04)", color: "rgba(242, 240, 247, 0.80)",
                  fontSize: 14, fontWeight: 500, border: "none", cursor: "pointer",
                  fontFamily: "var(--font-sans)", transition: "color .15s",
                }}
                onMouseEnter={(e) => (e.currentTarget.style.color = "#fff")}
                onMouseLeave={(e) => (e.currentTarget.style.color = "rgba(242,240,247,0.80)")}
              >
                See how it works
              </button>
            </div>

            {/* Social proof */}
            <div style={{ display: "flex", alignItems: "center", gap: 12, marginTop: 30 }}>
              <div style={{ display: "flex" }}>
                {(["#A78BFA","#E879F9","#FFB547","#6EE7B7","#60A5FA"] as const).map((c, i) => (
                  <div key={i} style={{
                    width: 26, height: 26, borderRadius: "50%", background: c,
                    border: "2px solid hsl(260 90% 2%)",
                    marginLeft: i === 0 ? 0 : -8,
                    display: "flex", alignItems: "center", justifyContent: "center",
                    fontSize: 9, fontWeight: 800, color: "#000", fontFamily: "var(--font-mono)",
                  }}>
                    {["L","M","R","K","A"][i]}
                  </div>
                ))}
              </div>
              <span style={{ fontFamily: "var(--font-sans)", fontSize: 13, color: "rgba(242,240,247,0.45)" }}>
                <span style={{ color: "rgba(242,240,247,0.80)", fontWeight: 600 }}>240+</span> teams already building
              </span>
            </div>
          </div>

          {/* Right: canvas illustration */}
          <div style={{ flex: 1, display: "flex", justifyContent: "center", alignItems: "center", position: "relative", zIndex: 12 }}>
            <CanvasIllustration />
          </div>
        </div>

        {/* Logo marquee */}
        <LogoMarquee />
      </div>
    </section>
  );
}

// ── Canvas illustration ───────────────────────────────────────────────────
const NODE_W = 122;
const NODE_H = 52;

type IllustrationNode = {
  x: number; y: number;
  type: "trigger" | "agent" | "tool402" | "action" | "end";
  name: string; sub: string; color: string;
  badge?: string; badgeColor?: string;
};

function CanvasIllustration() {
  const CW = 504, CH = 300; // canvas dimensions

  const nodes: IllustrationNode[] = [
    { x: 10,  y: 122, type: "trigger", name: "HTTP Trigger",    sub: "POST /webhook",       color: "#60A5FA" },
    { x: 155, y: 56,  type: "agent",   name: "Research Agent",  sub: "claude-opus-4-8",     color: "#A78BFA", badge: "thinking…", badgeColor: "#A78BFA" },
    { x: 155, y: 184, type: "tool402", name: "Weather API",     sub: "x402 · GoPlausible",  color: "#E879F9", badge: "⚡ paying", badgeColor: "#E879F9" },
    { x: 300, y: 122, type: "action",  name: "Send Report",     sub: "Resend · email",      color: "#FFB547" },
    { x: 428, y: 122, type: "end",     name: "Done",            sub: "",                    color: "#6EE7B7" },
  ];

  // Bezier paths between nodes
  // center-y = node.y + NODE_H/2 = node.y + 26
  const edgePaths = [
    // Trigger right(132,148) → Agent left(155,82)
    { d: `M 132,148 C 150,148 143,82 155,82`,  color: "#A78BFA", glow: true  },
    // Agent right(277,82) → Action left(300,148)
    { d: `M 277,82 C 296,82 290,148 300,148`,  color: "#A78BFA", glow: true  },
    // Agent bottom-center(216,108) → Tool top-center(216,184)
    { d: `M 216,108 C 216,146 216,162 216,184`, color: "#E879F9", glow: false },
    // Action right(422,148) → End left(428,148)
    { d: `M 422,148 L 428,148`,                 color: "#6EE7B7", glow: false },
  ];

  return (
    <div style={{ position: "relative", flexShrink: 0 }}>
      {/* Ambient glow behind the window */}
      <div style={{
        position: "absolute", top: "50%", left: "50%",
        transform: "translate(-50%,-50%)",
        width: 480, height: 280,
        background: "radial-gradient(ellipse, rgba(139,92,246,0.12) 0%, rgba(167,140,250,0.04) 55%, transparent 80%)",
        filter: "blur(32px)", pointerEvents: "none", borderRadius: "50%",
      }} />

      {/* Window frame */}
      <div style={{
        position: "relative", width: CW, height: CH + 40,
        borderRadius: 14, overflow: "hidden",
        border: "1px solid rgba(255,255,255,0.11)",
        boxShadow: "0 32px 80px rgba(0,0,0,0.65), 0 0 0 1px rgba(139,92,246,0.07), inset 0 1px 0 rgba(255,255,255,0.06)",
        background: "#08070C",
      }}>
        {/* Title bar */}
        <div style={{
          height: 40, background: "#0C0B16",
          borderBottom: "1px solid rgba(255,255,255,0.07)",
          display: "flex", alignItems: "center", padding: "0 14px", gap: 10,
        }}>
          <div style={{ display: "flex", gap: 6 }}>
            {(["#FF5F57","#FFBD2E","#28C840"] as const).map((c,i) => (
              <div key={i} style={{ width: 10, height: 10, borderRadius: "50%", background: c, opacity: 0.85 }} />
            ))}
          </div>
          <div style={{ flex: 1, textAlign: "center", fontFamily: "var(--font-mono)", fontSize: 10.5, color: "rgba(255,255,255,0.35)", letterSpacing: "0.01em" }}>
            Research Pipeline — AgentMesh Canvas
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <div style={{
              padding: "2px 8px", borderRadius: 5,
              background: "rgba(167,140,250,0.14)", border: "1px solid rgba(167,140,250,0.28)",
              fontFamily: "var(--font-mono)", fontSize: 9, color: "#A78BFA", letterSpacing: "0.04em",
            }}>DEPLOYED</div>
          </div>
        </div>

        {/* Canvas area */}
        <div style={{ position: "relative", width: CW, height: CH }}>
          {/* Dot grid */}
          <div className="canvas-bg" style={{ position: "absolute", inset: 0, backgroundSize: "22px 22px", opacity: 0.55 }} />

          {/* SVG edge layer */}
          <svg style={{ position: "absolute", inset: 0, width: "100%", height: "100%", pointerEvents: "none", zIndex: 1, overflow: "visible" }}>
            <defs>
              <filter id="illus-glow" x="-50%" y="-50%" width="200%" height="200%">
                <feGaussianBlur stdDeviation="2.5" result="blur" />
                <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
              </filter>
            </defs>
            {edgePaths.map((e, i) => (
              <g key={i}>
                {/* Glow underneath */}
                {e.glow && (
                  <path d={e.d} fill="none" stroke={e.color} strokeWidth={4} opacity={0.12} filter="url(#illus-glow)" />
                )}
                {/* Main edge — draw-in animation */}
                <path
                  d={e.d} fill="none" stroke={e.color} strokeWidth={1.5} opacity={0.75}
                  strokeDasharray="350"
                  style={{ animation: `draw-edge 1.0s cubic-bezier(.4,0,.2,1) ${i * 0.22}s both` }}
                />
                {/* Traveling signal on main edges */}
                {e.glow && (
                  <path
                    d={e.d} fill="none" stroke={e.color} strokeWidth={2.5}
                    strokeDasharray="12 200" opacity={0.9}
                    style={{ animation: `signal-flow 2.2s ease-in-out ${1.2 + i * 0.5}s infinite` }}
                  />
                )}
              </g>
            ))}
          </svg>

          {/* Nodes */}
          {nodes.map((node) => (
            <IllusNode key={node.type} node={node} />
          ))}

          {/* Floating run stats chip */}
          <div style={{
            position: "absolute", bottom: 12, right: 12, zIndex: 10,
            padding: "6px 12px", borderRadius: 8,
            background: "rgba(10,9,20,0.92)", border: "1px solid rgba(255,255,255,0.09)",
            backdropFilter: "blur(10px)",
            display: "flex", alignItems: "center", gap: 8,
            boxShadow: "0 4px 16px rgba(0,0,0,0.5)",
          }}>
            <div style={{ width: 6, height: 6, borderRadius: "50%", background: "#28C840", boxShadow: "0 0 6px #28C840", animation: "paying-blink 1.8s ease-in-out infinite" }} />
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "rgba(255,255,255,0.55)" }}>
              Run #12 · 1.8s · <span style={{ color: "#E879F9" }}>0.003 ALGO</span> paid
            </span>
          </div>

          {/* Wallet balance chip top-left */}
          <div style={{
            position: "absolute", top: 10, left: 10, zIndex: 10,
            padding: "5px 10px", borderRadius: 7,
            background: "rgba(10,9,20,0.88)", border: "1px solid rgba(255,181,71,0.2)",
            display: "flex", alignItems: "center", gap: 6,
          }}>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "rgba(255,181,71,0.5)", letterSpacing: "0.05em" }}>WALLET</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "#FFB547", fontWeight: 600 }}>4.20 ALGO</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function IllusNode({ node }: { node: IllustrationNode }) {
  const isAgent  = node.type === "agent";
  const isTool   = node.type === "tool402";
  const isEnd    = node.type === "end";
  const endW     = 62;

  const glyphs: Record<string, string> = {
    trigger: "⚡", agent: "◎", tool402: "⟡", action: "✉", end: "◈",
  };

  return (
    <div style={{
      position: "absolute", left: node.x, top: node.y,
      width: isEnd ? endW : NODE_W, height: NODE_H,
      background: "rgba(255,255,255,0.035)",
      border: `1px solid ${node.color}38`,
      borderRadius: 10, zIndex: 2,
      display: "flex", flexDirection: "column", justifyContent: "center",
      padding: isEnd ? "0 10px" : "0 11px 0 18px",
      boxShadow: `0 2px 20px ${node.color}0A`,
      ...(isAgent ? { animation: "node-pulse 2.4s ease-in-out infinite" } : {}),
    }}>
      {/* Left accent bar */}
      <div style={{
        position: "absolute", left: 0, top: 9, bottom: 9,
        width: 3, borderRadius: "0 2px 2px 0",
        background: node.color, opacity: 0.85,
        display: isEnd ? "none" : "block",
      }} />

      {isEnd ? (
        <div style={{ display: "flex", alignItems: "center", gap: 5 }}>
          <span style={{ color: node.color, fontSize: 11 }}>◈</span>
          <span style={{ fontFamily: "var(--font-sans)", fontSize: 11, fontWeight: 600, color: "rgba(255,255,255,0.65)", letterSpacing: "-0.01em" }}>Done</span>
        </div>
      ) : (
        <>
          <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span style={{ fontSize: 10, color: node.color, lineHeight: 1 }}>{glyphs[node.type]}</span>
            <span style={{ fontFamily: "var(--font-sans)", fontSize: 11, fontWeight: 600, color: "rgba(255,255,255,0.92)", letterSpacing: "-0.015em", whiteSpace: "nowrap" }}>{node.name}</span>
          </div>
          <div style={{ marginTop: 4 }}>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "rgba(255,255,255,0.30)", letterSpacing: "0.02em" }}>{node.sub}</span>
          </div>
        </>
      )}

      {/* Badge (thinking / paying) */}
      {node.badge && (
        <div style={{
          position: "absolute", top: -11, right: 6,
          padding: "2px 7px", borderRadius: 5,
          background: `${node.badgeColor}18`,
          border: `1px solid ${node.badgeColor}35`,
          fontFamily: "var(--font-mono)", fontSize: 8.5,
          color: node.badgeColor,
          animation: "paying-blink 1.4s ease-in-out infinite",
          whiteSpace: "nowrap",
        }}>{node.badge}</div>
      )}

      {/* Input port (left) */}
      {node.type !== "trigger" && !isEnd && (
        <div style={{ position: "absolute", left: -4, top: "50%", transform: "translateY(-50%)", width: 8, height: 8, borderRadius: "50%", background: node.color, border: "2px solid #08070C", zIndex: 3 }} />
      )}
      {/* Output port (right) */}
      {!isEnd && node.type !== "tool402" && (
        <div style={{ position: "absolute", right: -4, top: "50%", transform: "translateY(-50%)", width: 8, height: 8, borderRadius: "50%", background: node.color, border: "2px solid #08070C", zIndex: 3 }} />
      )}
      {/* End input port */}
      {isEnd && (
        <div style={{ position: "absolute", left: -4, top: "50%", transform: "translateY(-50%)", width: 8, height: 8, borderRadius: "50%", background: node.color, border: "2px solid #08070C", zIndex: 3 }} />
      )}
    </div>
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
