"use client";
import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter, usePathname } from "next/navigation";
import { Logo, Pill, Hairline, ghostBtnSm } from "@/components/ui";
import { useAuth } from "@/hooks/useAuth";

// Which chain settlements actually run on. Mainnet is the default because
// that is what the platform runs; overridable so a genuine testnet
// deployment doesn't have to lie in the other direction.
const ALGORAND_NETWORK = process.env.NEXT_PUBLIC_ALGORAND_NETWORK ?? "mainnet";

// Shared application top bar. Rendered identically on every authed page so the
// brand cluster, primary navigation, and account menu never drift between routes.
export function Topbar() {
  const router = useRouter();
  const pathname = usePathname();
  const { signOut, user, completeOnboarding } = useAuth();

  // Avatar shows the first letter of the signed-in user's name, falling back
  // to the email local part while auth is still loading or for an OAuth
  // account that hasn't completed onboarding yet.
  const initial = (
    user?.name?.trim()?.[0] ??
    user?.email?.trim()?.[0] ??
    "?"
  ).toUpperCase();

  const orgName = user?.orgName?.trim() || "Personal workspace";

  // Account menu opens two ways: hovering with a mouse (soft — closes shortly
  // after the pointer leaves the avatar and panel) or clicking/tapping (pinned —
  // survives mouse-leave, closes on outside press, Escape, or item selection).
  // Touch pointers skip the hover path entirely, so mobile is tap-only.
  const [menuState, setMenuState] = useState<"closed" | "hover" | "pinned">(
    "closed",
  );
  const menuOpen = menuState !== "closed";
  const menuRef = useRef<HTMLDivElement>(null);
  const hoverCloseTimer = useRef<number | null>(null);

  const cancelHoverClose = useCallback(() => {
    if (hoverCloseTimer.current != null) {
      window.clearTimeout(hoverCloseTimer.current);
      hoverCloseTimer.current = null;
    }
  }, []);

  const onMenuPointerEnter = (e: React.PointerEvent) => {
    if (e.pointerType === "touch") return;
    cancelHoverClose();
    setMenuState((s) => (s === "closed" ? "hover" : s));
  };
  // Grace delay so a brief slip off the menu doesn't snap it shut.
  const onMenuPointerLeave = (e: React.PointerEvent) => {
    if (e.pointerType === "touch") return;
    cancelHoverClose();
    hoverCloseTimer.current = window.setTimeout(() => {
      setMenuState((s) => (s === "hover" ? "closed" : s));
    }, 160);
  };

  useEffect(() => cancelHoverClose, [cancelHoverClose]);

  useEffect(() => {
    if (!menuOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenuState("closed");
    };
    const onPointer = (e: PointerEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node))
        setMenuState("closed");
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onPointer);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onPointer);
    };
  }, [menuOpen]);

  const handleSignOut = async () => {
    await signOut();
    router.push("/");
  };

  return (
    <div
      style={{
        height: 56,
        flexShrink: 0,
        background: "var(--bg-elev-1)",
        borderBottom: "1px solid var(--border)",
        padding: "0 24px",
        display: "flex",
        alignItems: "center",
        gap: 20,
      }}
    >
      {/* Brand + workspace context — one visual group */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <button
          onClick={() => router.push("/")}
          style={{
            background: "transparent",
            border: "none",
            cursor: "pointer",
            padding: 0,
          }}
        >
          <Logo size={18} />
        </button>
        <Hairline vertical length={22} />
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <button style={ghostBtnSm}>{orgName} ▾</button>
          <Pill mono dot tone="warm">
            {ALGORAND_NETWORK}
          </Pill>
        </div>
      </div>
      <div style={{ flex: 1 }} />
      {/* Navigation + account — the other group */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <nav style={{ display: "inline-flex", alignItems: "center", gap: 2 }}>
          <NavLink
            label="Workflows"
            active={pathname.startsWith("/workflows")}
            onClick={() => router.push("/workflows")}
          />
          <NavLink
            label="Usage"
            active={pathname.startsWith("/usage")}
            onClick={() => router.push("/usage")}
          />
          <NavLink
            label="Credits"
            active={pathname.startsWith("/billing")}
            onClick={() => router.push("/billing")}
          />
        </nav>
        <Hairline vertical length={22} />
        <div
          className="profile-menu"
          ref={menuRef}
          onPointerEnter={onMenuPointerEnter}
          onPointerLeave={onMenuPointerLeave}
        >
          <button
            className="profile-menu__trigger"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-label="Account menu"
            onClick={() =>
              setMenuState((s) => (s === "pinned" ? "closed" : "pinned"))
            }
          >
            {initial}
          </button>
          {menuOpen && (
            <div className="profile-menu__panel" role="menu">
              <div className="profile-menu__card">
                <div
                  style={{
                    padding: "12px 14px",
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                  }}
                >
                  <div
                    style={{
                      width: 28,
                      height: 28,
                      borderRadius: 999,
                      background: "var(--accent)",
                      color: "var(--accent-fg)",
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      fontSize: 11,
                      fontWeight: 700,
                      flexShrink: 0,
                    }}
                  >
                    {initial}
                  </div>
                  <div style={{ minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: 13,
                        fontWeight: 600,
                        color: "var(--fg)",
                      }}
                    >
                      {user?.name?.trim() || "—"}
                    </div>
                    <div
                      style={{
                        fontSize: 11,
                        color: "var(--fg-dim)",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {user?.email ?? "—"}
                    </div>
                  </div>
                </div>
                <div className="profile-menu__divider" />
                <button
                  className="profile-menu__item"
                  role="menuitem"
                  onClick={() => {
                    setMenuState("closed");
                    router.push("/settings");
                  }}
                >
                  Settings
                </button>
                <div className="profile-menu__divider" />
                <button
                  className="profile-menu__item profile-menu__item--danger"
                  role="menuitem"
                  onClick={() => {
                    setMenuState("closed");
                    handleSignOut();
                  }}
                >
                  Sign out
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
      {user?.needsOnboarding && (
        <OnboardingModal onComplete={completeOnboarding} />
      )}
    </div>
  );
}

// Shown once, right after a first-time OAuth sign-in — Google/GitHub only
// hand back a verified email, never a name or organization, so the account
// exists with both blank (see backend Me's needsOnboarding) until the user
// fills this in. Not dismissable: there's no cancel/skip, since every other
// account (password signup) already has both by the time it can sign in.
function OnboardingModal({
  onComplete,
}: {
  onComplete: (name: string, org: string) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [org, setOrg] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    setError("");
    try {
      await onComplete(name.trim(), org.trim());
    } catch {
      setError("Something went wrong. Please try again.");
      setSaving(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Finish setting up your account"
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0, 0, 0, 0.5)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 1000,
      }}
    >
      <form
        onSubmit={handleSubmit}
        style={{
          width: 340,
          background: "var(--bg-elev-1)",
          border: "1px solid var(--border)",
          borderRadius: "var(--r-2)",
          padding: 24,
          display: "flex",
          flexDirection: "column",
          gap: 14,
        }}
      >
        <div>
          <div style={{ fontSize: 16, fontWeight: 600, color: "var(--fg)" }}>
            Welcome — one more step
          </div>
          <div
            style={{ fontSize: 12.5, color: "var(--fg-muted)", marginTop: 4 }}
          >
            Tell us who you are so your teammates recognize you.
          </div>
        </div>
        <label
          style={{ display: "flex", flexDirection: "column", gap: 6 }}
        >
          <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>
            Full name
          </span>
          <input
            autoFocus
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Ada Lovelace"
            style={onboardingInputStyle}
          />
        </label>
        <label
          style={{ display: "flex", flexDirection: "column", gap: 6 }}
        >
          <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>
            Organization (optional)
          </span>
          <input
            value={org}
            onChange={(e) => setOrg(e.target.value)}
            placeholder="Acme Capital"
            style={onboardingInputStyle}
          />
        </label>
        {error && (
          <div
            style={{
              color: "var(--danger)",
              fontSize: 12,
              fontFamily: "var(--font-mono)",
            }}
          >
            {error}
          </div>
        )}
        <button
          type="submit"
          disabled={saving || !name.trim()}
          style={{
            height: 38,
            marginTop: 4,
            background: "var(--accent)",
            color: "var(--accent-fg)",
            border: "none",
            borderRadius: "var(--r-2)",
            fontSize: 13.5,
            fontWeight: 600,
            fontFamily: "var(--font-sans)",
            cursor: saving || !name.trim() ? "not-allowed" : "pointer",
            opacity: saving || !name.trim() ? 0.7 : 1,
          }}
        >
          {saving ? "Saving…" : "Continue"}
        </button>
      </form>
    </div>
  );
}

const onboardingInputStyle: React.CSSProperties = {
  height: 36,
  padding: "0 10px",
  background: "var(--bg-elev-2)",
  border: "1px solid var(--border)",
  borderRadius: "var(--r-2)",
  color: "var(--fg)",
  fontSize: 13,
  fontFamily: "var(--font-sans)",
};

// Top-bar navigation link. Active route is filled + full-contrast; others are
// muted and lighten on hover, so the bar always signals "you are here".
function NavLink({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      onMouseEnter={(e) => {
        if (!active) e.currentTarget.style.background = "var(--bg-elev-2)";
      }}
      onMouseLeave={(e) => {
        if (!active) e.currentTarget.style.background = "transparent";
      }}
      style={{
        height: 28,
        padding: "0 12px",
        fontSize: 12.5,
        fontWeight: 500,
        background: active ? "var(--bg-elev-3)" : "transparent",
        border: "none",
        borderRadius: "var(--r-2)",
        color: active ? "var(--fg)" : "var(--fg-muted)",
        cursor: "pointer",
        fontFamily: "var(--font-sans)",
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        transition: "background .15s var(--ease), color .15s var(--ease)",
      }}
    >
      {label}
    </button>
  );
}
