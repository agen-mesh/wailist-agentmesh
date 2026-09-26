import type { Page } from "@playwright/test";

export type Offender = { element: string; left: number; right: number };

/**
 * Opens a route and waits for it to load: the load event, then network idle
 * for at most five seconds.
 *
 * Network idle cannot be required. In mock mode the Bazaar consoles (Tendril,
 * Prism, HelixBox) request their service endpoints from the site itself, get
 * 404s, and those requests stay open, so idle never arrives even though the
 * page has rendered.
 */
export async function openRoute(page: Page, route: string) {
  const response = await page.goto(route, { waitUntil: "load" });
  await page
    .waitForLoadState("networkidle", { timeout: 5_000 })
    .catch(() => undefined);
  return response;
}

/** Collects uncaught exceptions thrown in the page from now on. */
export function collectPageErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("pageerror", (err) => errors.push(err.message));
  return errors;
}

/**
 * Finishes running animations, so a measurement is not taken mid-transition.
 * Infinite animations cannot be finished and are left alone.
 */
export async function settle(page: Page): Promise<void> {
  await page.evaluate(() => {
    for (const animation of document.getAnimations()) {
      try {
        animation.finish();
      } catch {
        // An infinite animation throws on finish(); it has no end state.
      }
    }
  });
}

/**
 * Visible elements that extend past either edge of the viewport.
 *
 * Elements are ignored when they sit inside:
 * - a container that scrolls horizontally on purpose (overflow-x auto or
 *   scroll), because its content is meant to be swiped;
 * - a container running an infinite CSS animation, such as the landing page's
 *   logo marquee, whose track is wider than the screen by design.
 * Elements inside an overflow-hidden container are not ignored: content cut
 * off by a clipping parent is the crop this check exists to catch. html and
 * body never count as scroll containers, so a page that scrolls sideways as a
 * whole is still reported.
 */
export async function horizontalOffenders(page: Page): Promise<Offender[]> {
  return page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth;

    // The element itself counts for the animation (the marquee track is the
    // animated element) but not for scrolling (a scroller that sticks out
    // past the edge is itself an overflow).
    const intentionallyOffscreen = (el: Element): boolean => {
      for (let n: Element | null = el; n; n = n.parentElement) {
        if (n === document.body || n === document.documentElement) break;
        const style = getComputedStyle(n);
        if (
          n !== el &&
          (style.overflowX === "auto" || style.overflowX === "scroll")
        ) {
          return true;
        }
        if (
          style.animationName !== "none" &&
          style.animationIterationCount
            .split(",")
            .some((count) => count.trim() === "infinite")
        ) {
          return true;
        }
      }
      return false;
    };

    const describe = (el: Element): string => {
      const firstClass = (el.getAttribute("class") ?? "")
        .trim()
        .split(/\s+/)[0];
      const label = el.getAttribute("aria-label");
      const text = (el.textContent ?? "").trim().slice(0, 30);
      return [
        el.tagName.toLowerCase(),
        firstClass ? `.${firstClass}` : "",
        label ? `[${label}]` : "",
        text ? ` "${text}"` : "",
      ].join("");
    };

    const offenders: { element: string; left: number; right: number }[] = [];
    for (const el of Array.from(document.body.querySelectorAll("*"))) {
      if (el.closest("[aria-hidden='true'], [inert]")) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.visibility === "hidden" || style.opacity === "0") continue;
      // A decorative layer with no text that cannot be touched, such as the
      // landing hero's blurred glow (105vw wide by design), crops nothing.
      if (style.pointerEvents === "none" && !(el.textContent ?? "").trim()) {
        continue;
      }
      const outside = rect.right > viewportWidth + 1 || rect.left < -1;
      if (outside && !intentionallyOffscreen(el)) {
        offenders.push({
          element: describe(el),
          left: Math.round(rect.left),
          right: Math.round(rect.right),
        });
      }
    }
    return offenders.slice(0, 20);
  });
}
