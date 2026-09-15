"use client";
import { useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useIsHandheld } from "@/hooks/useIsHandheld";
import { HANDHELD_TAB_ITEMS, isNavItemActive, type NavItem } from "@/lib/nav";
import { IconGrid } from "@/components/ui";

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
// Four destinations, taken from HANDHELD_TAB_ITEMS in lib/nav.ts. They differ
// from the desktop's APP_NAV_ITEMS on purpose: a handheld checks on workflows,
// so Activity has a tab and Usage and Credits are reached through Account.
// Material 3 puts a navigation bar at three to five destinations, so four needs
// no compromise.

// Only at the ROOT of each section, never on a pushed screen. A tab bar marks
// where you are among peers; on a detail screen the question is "how do I get
// back", which is what the back affordance answers. A workflow at
// /workflows/[id] is such a screen and carries its own way back to the list.
const TAB_ROOTS = new Set(
  HANDHELD_TAB_ITEMS.map((item) => item.href).filter(
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
      {HANDHELD_TAB_ITEMS.map((item) => {
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
// no business importing JSX. IconGrid already exists and already means the
// right thing; the others are drawn here because nothing in the set did.
function TabIcon({ item }: { item: NavItem }) {
  const size = 20;
  switch (item.href) {
    case "/bazaar":
      return <IconGrid size={size} />;
    case "/activity":
      return <IconPulse size={size} />;
    case "/account":
      return <IconPerson size={size} />;
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

const IconPulse = ({ size = 16 }: { size?: number }) => (
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
    <path
      d="M1.5 8h3l1.8-4.5 3.4 9 1.8-4.5h3"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const IconPerson = ({ size = 16 }: { size?: number }) => (
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
    <circle cx="8" cy="5.5" r="2.8" />
    <path
      d="M2.8 14c.6-2.8 2.7-4.3 5.2-4.3s4.6 1.5 5.2 4.3"
      strokeLinecap="round"
    />
  </svg>
);
