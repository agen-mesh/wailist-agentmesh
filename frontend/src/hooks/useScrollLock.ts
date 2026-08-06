"use client";
import { useEffect } from "react";

/**
 * Freezes page scroll while `locked` is true.
 *
 * Two details the naive version gets wrong:
 *
 * 1. It restores the *previous* inline overflow rather than clearing it, so a
 *    page that deliberately sets its own overflow is not trampled by the menu
 *    closing.
 * 2. It pins the scroll position. Setting `overflow: hidden` on the document
 *    discards the scroll offset in some engines, so the page jumps to the top
 *    when the lock lifts; capturing the offset and restoring it afterwards keeps
 *    the reader where they were.
 *
 * By default it locks both <html> and <body> — locking only one leaves iOS
 * Safari able to scroll the other. Pass `container` for a surface that scrolls
 * an element instead of the document: the landing page is `height: 100dvh;
 * overflow: hidden` with an inner `overflow-y: auto` div, so a document-level
 * lock there does nothing at all.
 *
 * Hiding the scrollbar also hands its width back to the layout, which shifts
 * everything still visible (the bar above the sheet) sideways. The gutter is
 * measured and re-added as padding so nothing moves.
 */
export function useScrollLock(
  locked: boolean,
  container?: React.RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!locked) return;

    const el = container?.current;
    const targets = el ? [el] : [document.documentElement, document.body];
    const prev = targets.map((t) => ({
      overflow: t.style.overflow,
      paddingRight: t.style.paddingRight,
    }));
    const scrollTop = el ? el.scrollTop : window.scrollY;

    // Width of the scrollbar that is about to disappear. Zero on overlay-
    // scrollbar platforms, ~8px here (see the ::-webkit-scrollbar rule).
    const gutter = el
      ? el.offsetWidth - el.clientWidth
      : window.innerWidth - document.documentElement.clientWidth;

    targets.forEach((t, i) => {
      t.style.overflow = "hidden";
      // Compensate on the element that owned the scrollbar — the last target,
      // since <body> is what paints inside <html>'s gutter.
      if (gutter > 0 && i === targets.length - 1) {
        const base = parseFloat(getComputedStyle(t).paddingRight) || 0;
        t.style.paddingRight = `${base + gutter}px`;
      }
    });

    return () => {
      targets.forEach((t, i) => {
        t.style.overflow = prev[i].overflow;
        t.style.paddingRight = prev[i].paddingRight;
      });
      if (el) el.scrollTop = scrollTop;
      else window.scrollTo(0, scrollTop);
    };
  }, [locked, container]);
}
