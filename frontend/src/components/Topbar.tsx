"use client";
import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter, usePathname } from "next/navigation";
import { Logo, Pill, Hairline, ghostBtnSm } from "@/components/ui";
import { useAuth } from "@/hooks/useAuth";

// Shared application top bar. Rendered identically on every authed page so the
// brand cluster, primary navigation, and account menu never drift between routes.
export function Topbar() {
  const router = useRouter();
  const pathname = usePathname();
  const { signOut, user } = useAuth();

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
          <button style={ghostBtnSm}>Acme Capital ▾</button>
          <Pill mono dot tone="warm">
            testnet
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
            AC
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
                    AC
                  </div>
                  <div style={{ minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: 13,
                        fontWeight: 600,
                        color: "var(--fg)",
                      }}
                    >
                      Acme Capital
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
                  onClick={() => setMenuState("closed")}
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
    </div>
  );
}

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
