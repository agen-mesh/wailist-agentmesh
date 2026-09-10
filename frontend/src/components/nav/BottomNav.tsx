"use client";
import { useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useIsHandheld } from "@/hooks/useIsHandheld";
import { APP_NAV_ITEMS, isNavItemActive, type NavItem } from "@/lib/nav";
import { IconGrid, IconWallet } from "@/components/ui";

// The app's navigation on a phone.
//
// Why this exists at all: the top bar collapses into a hamburger below 768px,
// and a hamburger is measurably the worst place to put primary navigation on a
// phone. NN/g's testing puts discovery of hidden navigation at roughly 21%
// against 48% for visible, with tasks taking about 2.5s longer and rated 15%
// harder. Reachability compounds it -- tap accuracy is around 96% at the bottom
// of a tall screen and around 61% at the top, which is exactly where a
// hamburger sits.
//
// Four destinations, taken from APP_NAV_ITEMS rather than a second list: this
// is the same navigation, shown where a thumb can reach it. Material 3 puts a
// navigation bar at three to five destinations, so four needs no compromise.

// Only at the ROOT of each section, never on a pushed screen. A tab bar marks
// where you are among peers; on a detail screen the question is "how do I get
// back", which is what the back affordance answers. The studio at
// /workflows/[id] also owns the bottom of the screen for its own sheet, and two
// bars stacked there would be worse than either alone.
const TAB_ROOTS = new Set(
  APP_NAV_ITEMS.map((item) => item.href).filter(
    (href): href is string => typeof href === "string",
  ),
);

export function BottomNav() {
  const handheld = useIsHandheld();
  const pathname = usePathname();
  const visible = handheld && TAB_ROOTS.has(pathname);

  // Tells the stylesheet to reserve room at the bottom of the app shell, so the
  // last row of a list is not sitting underneath a fixed bar. An attribute on
  // <body> rather than a prop threaded through five page components: the bar is
  // app-wide chrome and the pages should not each have to know it exists. The
  // cleanup runs on every change, so navigating to a detail screen releases the
  // space in the same frame the bar disappears.
  useEffect(() => {
    if (!visible) return;
    document.body.setAttribute("data-bottomnav", "");
    return () => document.body.removeAttribute("data-bottomnav");
  }, [visible]);

  // Renders nothing until hydration, on the server's desktop answer, by design.
  // See useIsHandheld: the alternative is markup that does not match.
  if (!visible) return null;

  return (
    <nav className="bottomnav am-safe-bottom" aria-label="Primary">
      {APP_NAV_ITEMS.map((item) => {
        const active = isNavItemActive(item, pathname);
        return (
          <Link
            key={item.label}
            href={item.href ?? "/"}
            className="bottomnav__item"
            // aria-current, not only colour: the active tab has to be
            // announced, not just seen.
            aria-current={active ? "page" : undefined}
            data-active={active ? "" : undefined}
          >
            <TabIcon item={item} />
            <span className="bottomnav__label">{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}

// Icons live here rather than in lib/nav.ts, which is a plain manifest and has
// no business importing JSX. IconGrid and IconWallet already exist and already
// mean the right things; the two below are new because nothing in the set did.
function TabIcon({ item }: { item: NavItem }) {
  const size = 20;
  switch (item.href) {
    case "/bazaar":
      return <IconGrid size={size} />;
    case "/billing":
      return <IconWallet size={size} />;
    case "/usage":
      return <IconChart size={size} />;
    default:
      return <IconNodes size={size} />;
  }
}

// Matching the set's idiom exactly: 16x16 viewBox, currentColor, 1.3 stroke.
const IconNodes = ({ size = 16 }: { size?: number }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.3"
    aria-hidden="true"
    style={{ display: "block" }}
  >
    <circle cx="3.5" cy="8" r="2" />
    <circle cx="12.5" cy="3.5" r="2" />
    <circle cx="12.5" cy="12.5" r="2" />
    <path d="M5.3 7.1 10.7 4.4M5.3 8.9l5.4 2.7" />
  </svg>
);

const IconChart = ({ size = 16 }: { size?: number }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.3"
    aria-hidden="true"
    style={{ display: "block" }}
  >
    <path d="M2 13.5V9M6 13.5V4M10 13.5V7M14 13.5V2" strokeLinecap="round" />
  </svg>
);
