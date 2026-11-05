import type { Metadata } from "next";

// What the public website shows while MAINTENANCE_MODE is on. middleware.ts
// rewrites every gated page here, so a visitor stays on the URL they asked for
// and sees this instead -- no landing page, no Sign In / Sign Up, no studio.
//
// Deliberately carries no auth or studio links: the point of maintenance mode
// is that nobody outside the team can start a session. The legal pages stay
// linked because they stay public (Google's OAuth verification needs /privacy
// reachable at all times).

export const metadata: Metadata = {
  title: "Under maintenance · AgentMesh",
  description:
    "AgentMesh is temporarily unavailable while we make improvements.",
};

const MAINTENANCE_CSS = `
.maintenance-container {
  min-height: 100vh;
  min-height: 100dvh;
}
.maintenance-logo {
  animation: float-y 6s ease-in-out infinite;
}
.maintenance-link {
  color: var(--fg-dim);
  text-decoration: none;
  border-radius: var(--r-1);
  transition: color 0.15s var(--ease);
}
.maintenance-link:hover {
  color: var(--fg);
  text-decoration: underline;
}
.maintenance-link:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
@media (prefers-reduced-motion: reduce) {
  .maintenance-logo,
  .maintenance-dot {
    animation: none;
  }
}
`;

export default function MaintenancePage() {
  return (
    <main className="maintenance-container" style={containerStyle}>
      <style>{MAINTENANCE_CSS}</style>
      <div style={glowBgStyle} />
      <div className="canvas-bg" style={gridBgStyle} />

      <div className="reveal" style={cardStyle}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/logo.png"
          alt="AgentMesh"
          width={96}
          height={96}
          className="maintenance-logo"
          style={logoMarkStyle}
        />

        <div style={pillStyle}>
          <span className="maintenance-dot pulse" style={dotStyle} />
          Under maintenance
        </div>

        <h1 style={headingStyle}>
          Agent<span className="hero-gradient">Mesh</span> will be back soon.
        </h1>

        <p style={descriptionStyle}>
          We&apos;re making improvements to the platform, so the website is
          temporarily unavailable. It will be available again upon further
          notice. Thank you for your patience.
        </p>

        <div style={dividerStyle} />

        <nav aria-label="Legal" style={linksStyle}>
          <a href="/terms" className="maintenance-link">
            Terms
          </a>
          <span aria-hidden style={separatorStyle}>
            ·
          </span>
          <a href="/privacy" className="maintenance-link">
            Privacy
          </a>
          <span aria-hidden style={separatorStyle}>
            ·
          </span>
          <a href="/refund-policy" className="maintenance-link">
            Refund policy
          </a>
        </nav>
      </div>
    </main>
  );
}

const containerStyle: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  justifyContent: "center",
  background: "var(--bg)",
  color: "var(--fg)",
  fontFamily: "var(--font-sans)",
  position: "relative",
  overflow: "hidden",
  padding: "24px 16px",
  boxSizing: "border-box",
};

const glowBgStyle: React.CSSProperties = {
  position: "absolute",
  width: "480px",
  height: "480px",
  background: "var(--accent-glow)",
  filter: "blur(120px)",
  borderRadius: "50%",
  top: "50%",
  left: "50%",
  transform: "translate(-50%, -50%)",
  pointerEvents: "none",
  opacity: 0.18,
};

const gridBgStyle: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  backgroundSize: "20px 20px",
  opacity: 0.1,
  pointerEvents: "none",
};

const cardStyle: React.CSSProperties = {
  background: "var(--bg-elev-1)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-4)",
  padding: "48px 32px 32px",
  maxWidth: "520px",
  width: "100%",
  boxSizing: "border-box",
  textAlign: "center",
  zIndex: 1,
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
};

const logoMarkStyle: React.CSSProperties = {
  marginBottom: "24px",
  borderRadius: "22%",
};

const pillStyle: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "8px",
  height: "28px",
  padding: "0 12px",
  borderRadius: "999px",
  border: "1px solid var(--border-strong)",
  background: "var(--bg-elev-2)",
  color: "var(--fg-muted)",
  fontSize: "12px",
  fontWeight: 500,
  letterSpacing: "0.02em",
  marginBottom: "20px",
};

const dotStyle: React.CSSProperties = {
  width: "7px",
  height: "7px",
  borderRadius: "50%",
  background: "var(--warm)",
  flexShrink: 0,
};

const headingStyle: React.CSSProperties = {
  fontSize: "clamp(26px, 6vw, 34px)",
  fontWeight: 600,
  letterSpacing: "-0.03em",
  lineHeight: 1.15,
  textWrap: "balance",
  margin: "0 0 14px 0",
};

const descriptionStyle: React.CSSProperties = {
  fontSize: "14.5px",
  lineHeight: 1.65,
  color: "var(--fg-muted)",
  maxWidth: "60ch",
  margin: 0,
};

const dividerStyle: React.CSSProperties = {
  width: "100%",
  height: "1px",
  background: "var(--border)",
  margin: "32px 0 20px",
};

const linksStyle: React.CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  justifyContent: "center",
  alignItems: "center",
  gap: "10px",
  fontSize: "12.5px",
};

const separatorStyle: React.CSSProperties = {
  color: "var(--fg-dim)",
};
