import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { STATUS, SURFACE, TYPE } from "./tokens";

// tokens.ts duplicates values globals.css declares. A duplicated constant
// nothing checks is drift waiting to happen: if this fails, one of the two
// was changed alone.

const css = readFileSync(join(__dirname, "../app/globals.css"), "utf8");

function cssVar(name: string): string | undefined {
  return new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`).exec(css)?.[1];
}

describe("design tokens", () => {
  it.each(Object.entries(STATUS))("--%s matches globals.css", (name, hex) => {
    expect(cssVar(name)).toBe(hex);
  });

  // camelCase key -> kebab-case custom property (x402Fg -> --type-x402-fg).
  it.each(Object.entries(TYPE))(
    "--type-%s matches globals.css",
    (name, hex) => {
      const prop = name.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
      expect(cssVar(`type-${prop}`)).toBe(hex);
    },
  );

  // Values something outside CSS is handed: xterm's canvas, and the
  // theme-color meta tag.
  it("--bg-elev-1 matches the surface xterm is handed", () => {
    expect(cssVar("bg-elev-1")).toBe(SURFACE.elev1);
  });

  it("--bg matches the theme-color the browser paints its chrome with", () => {
    expect(cssVar("bg")).toBe(SURFACE.bg);
  });
});

// The contrast ladder, as a mechanism rather than a comment. The first pass
// stated its ratios in a comment and got two of them wrong -- --fg-dim read
// 4.49 on elev-2 and 4.09 on elev-3 while the block above it claimed the
// ladder cleared AA. A number in a comment is not checked by anything.
//
// Measured against EVERY surface a foreground token can land on, not just
// --bg: an eyebrow inside a panel inside a card is sitting on elev-3, and
// 1.4.3 relaxes to 3:1 only for large text, which none of these carry.

function srgbToLinear(channel: number): number {
  return channel <= 0.03928
    ? channel / 12.92
    : Math.pow((channel + 0.055) / 1.055, 2.4);
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) =>
    srgbToLinear(parseInt(hex.slice(i, i + 2), 16) / 255),
  );
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const SURFACES = ["bg", "bg-elev-1", "bg-elev-2", "bg-elev-3"] as const;
const FOREGROUNDS = ["fg", "fg-muted", "fg-dim"] as const;
const AA_NORMAL = 4.5;

describe("foreground contrast ladder", () => {
  const pairs = FOREGROUNDS.flatMap((fg) =>
    SURFACES.map((surface) => [fg, surface] as const),
  );

  it.each(pairs)("--%s on --%s clears AA for normal text", (fg, surface) => {
    const a = cssVar(fg);
    const b = cssVar(surface);
    expect(a, `--${fg} must be a 6-digit hex in globals.css`).toBeDefined();
    expect(b, `--${surface} must be a 6-digit hex in globals.css`).toBeDefined();
    expect(contrast(a!, b!)).toBeGreaterThanOrEqual(AA_NORMAL);
  });

  // Three steps have to stay three steps. Raising --fg-dim to clear AA closes
  // the gap to --fg-muted; past roughly 1.3:1 the two stop reading as
  // different, which is the trade the ladder above is balancing.
  it("keeps --fg-muted and --fg-dim visibly apart", () => {
    expect(contrast(cssVar("fg-muted")!, cssVar("fg-dim")!)).toBeGreaterThan(
      1.35,
    );
  });
});
