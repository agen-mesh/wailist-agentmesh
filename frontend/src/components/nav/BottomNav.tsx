"use client";
import { useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useIsHandheld } from "@/hooks/useIsHandheld";
import { APP_NAV_ITEMS, isNavItemActive, type NavItem } from "@/lib/nav";
import { IconGrid, IconWallet } from "@/components/ui";
import { watchKeyboard } from "@/native/keyboard";

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

  // While the on-screen keyboard is up for a text field, the bar steps aside
  // (body[data-typing] in globals.css). The WebView shrinks to the space above
  // the keyboard, and a fixed bar would sit over the field.
  useEffect(() => {
    if (!visible) return;
    let live = true;
    const textTypes = new Set([
      "text",
      "search",
      "email",
      "password",
      "number",
      "tel",
      "url",
    ]);
    // Focus alone does not mean the keyboard is up: Back on Android closes the
    // keyboard and leaves the field focused, and the bar used to stay hidden
    // with nothing on screen to hide it for.
    //
    // In the app, Android says when the keyboard opens and closes, and that
    // is the answer -- a rotation that closes the keyboard is reported like
    // any other close.
    let keyboardOpen = false;
    const stopWatching = watchKeyboard((open) => {
      keyboardOpen = open;
      sync();
    });
    // A browser says nothing, so there the keyboard is read from the
    // viewport, which shrinks by the keyboard's height when it opens, measured
    // against the tallest height seen at this width.
    //
    // Each width keeps its own tallest height, so rotating back into an
    // orientation already seen has its baseline at once. One not yet seen
    // with the keyboard shut, reached with the keyboard up, starts unknown --
    // keyboard assumed up, since the first height there is already shrunk by
    // it -- until the height jumps back (the keyboard closed) or focus leaves
    // the field.
    const KEYBOARD_MIN_PX = 150;
    const height = () => window.visualViewport?.height ?? window.innerHeight;
    const tallestAt = new Map<number, number>();
    let width = window.innerWidth;
    let tallest: number | null = height();
    let last = height();
    const measuredKeyboardUp = (textField: boolean) => {
      const h = height();
      if (window.innerWidth !== width) {
        width = window.innerWidth;
        const keyboardWasUp = document.body.hasAttribute("data-typing");
        tallest =
          tallestAt.get(width) ?? (keyboardWasUp && textField ? null : h);
      }
      if (tallest === null && (!textField || h - last > KEYBOARD_MIN_PX)) {
        tallest = h;
      }
      if (tallest !== null) {
        tallest = Math.max(tallest, h);
        tallestAt.set(width, tallest);
      }
      last = h;
      return tallest === null ? true : tallest - h > KEYBOARD_MIN_PX;
    };
    const sync = () => {
      if (!live) return;
      const el = document.activeElement;
      const textField =
        (el instanceof HTMLInputElement &&
          textTypes.has(el.type) &&
          !el.readOnly) ||
        (el instanceof HTMLTextAreaElement && !el.readOnly) ||
        (el instanceof HTMLElement && el.isContentEditable);
      const keyboardUp = stopWatching
        ? keyboardOpen
        : measuredKeyboardUp(textField);
      if (textField && keyboardUp) {
        document.body.setAttribute("data-typing", "");
      } else {
        document.body.removeAttribute("data-typing");
      }
    };
    // focusout fires before focus reaches the next element, so the new focus
    // is read on the next task.
    const onFocusOut = () => {
      window.setTimeout(sync, 0);
    };
    document.addEventListener("focusin", sync);
    document.addEventListener("focusout", onFocusOut);
    window.addEventListener("resize", sync);
    window.visualViewport?.addEventListener("resize", sync);
    return () => {
      live = false;
      stopWatching?.();
      document.removeEventListener("focusin", sync);
      document.removeEventListener("focusout", onFocusOut);
      window.removeEventListener("resize", sync);
      window.visualViewport?.removeEventListener("resize", sync);
      document.body.removeAttribute("data-typing");
    };
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
