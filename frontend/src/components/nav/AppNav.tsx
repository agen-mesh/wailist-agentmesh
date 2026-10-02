"use client";
import { Fragment, useEffect, useId, useRef, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { type NavItem, groupNavItems, isNavItemActive } from "@/lib/nav";
import { useScrollLock } from "@/hooks/useScrollLock";
import { useCloseOnBack } from "@/hooks/useCloseOnBack";

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

// The surface arrives first, then the rows stagger in behind it. Exit is
// faster and NOT staggered: a menu closing is the answer to a tap that already
// happened, so nobody should wait for six rows to leave one at a time.
const SHEET_IN = { type: "spring" as const, stiffness: 420, damping: 38, mass: 0.9 };
const SHEET_OUT = { duration: 0.16, ease: [0.4, 0, 1, 1] as const };

const listVariants = {
  closed: {},
  open: { transition: { staggerChildren: 0.035, delayChildren: 0.04 } },
};
const rowVariants = {
  closed: { opacity: 0, y: -8 },
  open: {
    opacity: 1,
    y: 0,
    transition: { type: "spring" as const, stiffness: 520, damping: 40 },
  },
};

/**
 * The navigation shell for both the marketing and application surfaces: an
 * ordinary bar with inline links above `md`, a trigger and a sheet below it.
 *
 * ARIA disclosure, not `role="menu"` — a menu role would promise arrow-key
 * traversal a list of links does not have. Which surface shows is decided in
 * CSS, so there is no useMediaQuery and no hydration mismatch. The sheet is
 * mounted only while open, so nothing can sit under it when it is shut.
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
  const sheetRef = useRef<HTMLDivElement>(null);
  // Less motion is not a request for faster motion: this removes it entirely.
  const still = useReducedMotion();

  useScrollLock(open, scrollContainer);
  // Back closes the sheet rather than leaving the page under it. Every path
  // that closes the sheet sets `open` to false, and the hook removes its
  // history entry when that happens, so its return value is not needed.
  useCloseOnBack(() => setOpen(false), open);

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

  // Focus leaving the trigger/sheet closes it. Scoped to those two rather than
  // the whole root: `actions` (e.g. the account menu trigger) renders inside
  // the same root but is not part of this disclosure, so focus moving there
  // must close the sheet rather than being treated as "still inside".
  useEffect(() => {
    if (!open) return;
    const onFocusIn = (e: FocusEvent) => {
      const target = e.target as Node;
      if (triggerRef.current?.contains(target)) return;
      if (sheetRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("focusin", onFocusIn);
    return () => document.removeEventListener("focusin", onFocusIn);
  }, [open]);

  // Above the breakpoint the trigger is display:none, so an `open` that
  // survives is an invisible scroll lock with nothing left to release it.
  // `change`, not `resize`: a viewport can cross a breakpoint without resizing.
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 768px)");
    const closeAboveBreakpoint = () => {
      if (!mq.matches) setOpen(false);
    };
    closeAboveBreakpoint();
    mq.addEventListener("change", closeAboveBreakpoint);
    return () => mq.removeEventListener("change", closeAboveBreakpoint);
  }, []);

  const select = (item: NavItem) => {
    setOpen(false);
    onSelect(item);
  };

  const groups = groupNavItems(items);

  return (
    <div
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
            // Only while the sheet exists: it is mounted on open now, and
            // aria-controls pointing at an absent id breaks the disclosure
            // relationship rather than describing it.
            aria-controls={open ? sheetId : undefined}
            aria-label={open ? "Close menu" : "Menu"}
            onClick={() => setOpen((o) => !o)}
          >
            <BurgerIcon open={open} still={!!still} />
          </button>
        </div>

        <AnimatePresence initial={false}>
          {open && (
            <motion.div
              ref={sheetRef}
              id={sheetId}
              className="appnav__sheet"
              initial={still ? { opacity: 0 } : { opacity: 0, y: -10 }}
              animate={still ? { opacity: 1 } : { opacity: 1, y: 0 }}
              // Its own shorter curve: a spring would ring on the way out.
              exit={
                still
                  ? { opacity: 0, transition: { duration: 0 } }
                  : { opacity: 0, y: -6, transition: SHEET_OUT }
              }
              transition={still ? { duration: 0 } : SHEET_IN}
            >
              <motion.nav
                aria-label="Primary"
                variants={still ? undefined : listVariants}
                initial="closed"
                animate="open"
              >
                {groups.map(({ group, items: groupItems }) => (
                  <div key={group || "_"} className="appnav__section">
                    {group ? (
                      <motion.div
                        className="appnav__group"
                        variants={still ? undefined : rowVariants}
                      >
                        {group}
                      </motion.div>
                    ) : null}
                    <ul className="appnav__list">
                      {groupItems.map((item) => {
                        const active = isNavItemActive(item, pathname);
                        return (
                          <motion.li
                            key={item.label}
                            variants={still ? undefined : rowVariants}
                          >
                            <button
                              type="button"
                              className="appnav__link"
                              data-active={active || undefined}
                              aria-current={active ? "page" : undefined}
                              onClick={() => select(item)}
                            >
                              <span className="appnav__link-label">
                                {item.label}
                              </span>
                              <ChevronRight />
                            </button>
                          </motion.li>
                        );
                      })}
                    </ul>
                  </div>
                ))}
              </motion.nav>
              {sheetFooter ? (
                <motion.div
                  className="appnav__sheet-foot"
                  initial={still ? undefined : { opacity: 0 }}
                  animate={still ? undefined : { opacity: 1 }}
                  transition={{ delay: 0.12, duration: 0.2 }}
                >
                  {sheetFooter}
                </motion.div>
              ) : null}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}

// A full-width row that does something when pressed should say which way it
// goes; without this the sheet was plain text that happened to be tappable.
function ChevronRight() {
  return (
    <svg
      className="appnav__chevron"
      width="14"
      height="14"
      viewBox="0 0 16 16"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M6 3.5 L10.5 8 L6 12.5"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// Two strokes that cross into an X. Rotated about a shared centre rather than
// animating x1/y1/x2/y2, which stretched both lines to ~19px at the crossed
// state and made the X visibly bigger than the burger it came from.
function BurgerIcon({ open, still }: { open: boolean; still: boolean }) {
  const spring = still
    ? { duration: 0 }
    : { type: "spring" as const, stiffness: 500, damping: 30 };
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
      <motion.line
        x1="2.5"
        x2="15.5"
        y1="9"
        y2="9"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        style={{ originX: "9px", originY: "9px" }}
        initial={false}
        animate={{ rotate: open ? 45 : 0, y: open ? 0 : -3.5 }}
        transition={spring}
      />
      <motion.line
        x1="2.5"
        x2="15.5"
        y1="9"
        y2="9"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        style={{ originX: "9px", originY: "9px" }}
        initial={false}
        animate={{ rotate: open ? -45 : 0, y: open ? 0 : 3.5 }}
        transition={spring}
      />
    </svg>
  );
}
