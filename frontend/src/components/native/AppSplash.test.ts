import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { appHasPainted } from "./AppSplash";

// jsdom does not lay out, so getBoundingClientRect is 0x0 for everything and
// the height half of the predicate cannot be exercised for free. Height is
// stubbed from a data-h attribute, which keeps the assertions about the part
// that actually has logic in it -- what counts as content -- rather than about
// a layout engine that is not present.
const realRect = Element.prototype.getBoundingClientRect;

beforeEach(() => {
  Element.prototype.getBoundingClientRect = function () {
    const h = Number((this as HTMLElement).dataset?.h ?? "0");
    return { height: h, width: h ? 100 : 0 } as DOMRect;
  };
  document.body.innerHTML = "";
});

afterEach(() => {
  Element.prototype.getBoundingClientRect = realRect;
});

const body = () => document.body;

describe("appHasPainted", () => {
  it("is false with only the splash on screen", () => {
    body().innerHTML = `<div class="splash" data-h="800"><span>AgentMesh</span></div>`;
    expect(appHasPainted(body())).toBe(false);
  });

  // The regression this predicate exists for. The static export's first frame
  // carries a full-height but empty placeholder, and the old "height > 0" test
  // accepted it -- so the splash left while the screen behind it was blank.
  it("is false for the full-height empty placeholder the export ships", () => {
    body().innerHTML =
      `<div class="splash" data-h="800"></div>` +
      `<div data-h="800"><div data-h="800" style="min-height:100dvh"></div></div>`;
    expect(appHasPainted(body())).toBe(false);
  });

  it("is true once a screen has rendered text", () => {
    body().innerHTML =
      `<div class="splash" data-h="800"></div>` +
      `<div data-h="800"><main data-h="800"><h1>Sign in</h1></main></div>`;
    expect(appHasPainted(body())).toBe(true);
  });

  // A screen can be legitimately wordless -- an icon-only state, a chart.
  it("is true for content that is not text", () => {
    body().innerHTML =
      `<div class="splash" data-h="800"></div>` +
      `<div data-h="800"><svg data-h="40"></svg></div>`;
    expect(appHasPainted(body())).toBe(true);
  });

  // Scripts and the route announcer sit in <body> with content but no box.
  it("ignores children that occupy no space", () => {
    body().innerHTML =
      `<div class="splash" data-h="800"></div>` +
      `<next-route-announcer data-h="0">Sign in</next-route-announcer>`;
    expect(appHasPainted(body())).toBe(false);
  });
});
