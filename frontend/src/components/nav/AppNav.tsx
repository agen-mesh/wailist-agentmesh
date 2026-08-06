"use client";
import { Fragment, useEffect, useId, useRef, useState } from "react";
import { type NavItem, groupNavItems, isNavItemActive } from "@/lib/nav";
import { useScrollLock } from "@/hooks/useScrollLock";

interface AppNavProps {
  items: readonly NavItem[];
  /** Brand cluster, pinned to the start of the bar. */
  brand: React.ReactNode;
  /** Trailing controls (account menu, CTA). Stays visible at every width. */
  actions?: React.ReactNode;
  /** Current pathname, for active state. Landing pages can pass "". */
  pathname: string;
  onSelect: (item: NavItem) => void;
  /** Renders the inline links; lets each surface keep its own link styling. */
  renderInlineLink: (args: {
    item: NavItem;
    active: boolean;
    onClick: () => void;
  }) => React.ReactNode;
  /**
   * Visual treatment. `app` is the solid 56px bar on the authed shell;
   * `landing` is the taller transparent bar over the marketing hero. Only the
   * skin differs — layout, semantics and motion are shared.
   */
  variant?: "app" | "landing";
  /**
   * Appended below the links in the sheet. For a CTA that lives in the bar at
   * wide widths and needs somewhere to go once the bar drops it. Rendered
   * outside the <nav>, because a call to action is not wayfinding.
   */
  sheetFooter?: React.ReactNode;
  /**
   * The element that actually scrolls, when it is not the document. The landing
   * page scrolls an inner `overflow-y: auto` div, so locking <html>/<body>
   * there would silently do nothing.
   */
  scrollContainer?: React.RefObject<HTMLElement | null>;
}

/**
 * The navigation shell for both the marketing and application surfaces.
 *
 * Above the `md` breakpoint it is an ordinary bar with inline links. Below it,
 * the links are replaced by a trigger and a full-height sheet fades in beneath
 * the bar (see the AppNav block in globals.css for the motion, and for why the
 * sheet is fixed rather than a grown bar).
 *
 * Semantics follow the ARIA disclosure pattern, not `role="menu"`: a real
 * <button> with aria-expanded/aria-controls revealing a <nav> of links. A menu
 * role would promise arrow-key traversal that a list of links does not have.
 *
 * Which surface is showing is decided entirely in CSS, so there is no
 * useMediaQuery, no hydration mismatch, and no flash of the wrong layout. The
 * sheet's markup is always rendered; `visibility` (stepped, not interpolated)
 * is what takes it out of hit-testing and the accessibility tree.
 */
export function AppNav({
  items,
  brand,
  actions,
  pathname,
  onSelect,
  renderInlineLink,
  variant = "app",
  sheetFooter,
  scrollContainer,
}: AppNavProps) {
  const [open, setOpen] = useState(false);
  const sheetId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  useScrollLock(open, scrollContainer);

  // Escape closes and returns focus to the trigger, per the disclosure pattern.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  // Focus leaving the navigation region closes it.
  useEffect(() => {
    if (!open) return;
    const onFocusIn = (e: FocusEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("focusin", onFocusIn);
    return () => document.removeEventListener("focusin", onFocusIn);
  }, [open]);

  // Crossing the breakpoint must not play the open animation. The class is
  // added for one frame around each resize, matching the escape hatch the
  // reference implementation ships.
  useEffect(() => {
    let raf = 0;
    const onResize = () => {
      document.documentElement.classList.add("appnav-block-transitions");
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        document.documentElement.classList.remove("appnav-block-transitions");
      });
    };
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      cancelAnimationFrame(raf);
      document.documentElement.classList.remove("appnav-block-transitions");
    };
  }, []);

  const select = (item: NavItem) => {
    setOpen(false);
    onSelect(item);
  };

  return (
    <>
      <div
        ref={rootRef}
        className={`appnav appnav--${variant}${open ? " appnav--open" : ""}`}
        data-open={open || undefined}
      >
        <div className="appnav__shell">
          <div className="appnav__bar">
            {brand}
            <div className="appnav__spacer" />
            <nav className="appnav__inline" aria-label="Primary">
              {/* Keyed here rather than in renderInlineLink: `href` is optional
                  on a NavItem, so a consumer keying by it silently produces
                  undefined keys for the landing page's section links. */}
              {items.map((item) => (
                <Fragment key={item.label}>
                  {renderInlineLink({
                    item,
                    active: isNavItemActive(item, pathname),
                    onClick: () => select(item),
                  })}
                </Fragment>
              ))}
            </nav>
            {actions ? <div className="appnav__actions">{actions}</div> : null}
            <button
              ref={triggerRef}
              type="button"
              className="appnav__trigger"
              aria-expanded={open}
              aria-controls={sheetId}
              aria-label={open ? "Close menu" : "Menu"}
              onClick={() => setOpen((o) => !o)}
            >
              <BurgerIcon open={open} />
            </button>
          </div>

          <div id={sheetId} className="appnav__sheet">
            <nav aria-label="Primary">
              {groupNavItems(items).map(({ group, items: groupItems }) => (
                <div key={group || "_"}>
                  {group ? <div className="appnav__group">{group}</div> : null}
                  <ul style={{ listStyle: "none", margin: 0, padding: 0 }}>
                    {groupItems.map((item) => (
                      <li key={item.label}>
                        <button
                          type="button"
                          className="appnav__link"
                          aria-current={
                            isNavItemActive(item, pathname) ? "page" : undefined
                          }
                          onClick={() => select(item)}
                        >
                          {item.label}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </nav>
            {sheetFooter ? (
              <div className="appnav__sheet-foot">{sheetFooter}</div>
            ) : null}
          </div>
        </div>
      </div>
      {/* Sibling, so the open-state class on .appnav drives it through `~` with
          no second piece of state to keep in sync. */}
      <div className="appnav__scrim" onClick={() => setOpen(false)} />
    </>
  );
}

// Two strokes that morph into an X. The endpoints are animated rather than the
// whole glyph rotated, so the caps stay put and the cross lands centred.
function BurgerIcon({ open }: { open: boolean }) {
  const stroke = {
    transition: "all var(--nav-rate-fade) var(--ease-nav)",
  } as const;
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
      <line
        x1="2"
        y1={open ? "4" : "6"}
        x2="16"
        y2={open ? "14" : "6"}
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        style={stroke}
      />
      <line
        x1="2"
        y1={open ? "14" : "12"}
        x2="16"
        y2={open ? "4" : "12"}
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        style={stroke}
      />
    </svg>
  );
}
