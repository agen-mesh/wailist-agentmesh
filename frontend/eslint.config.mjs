import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

// ── Design-token rules ──────────────────────────────────────────────────────
// Almost everything here is styled with inline React styles, which no
// stylesheet convention can police. These keep the token layer from being
// re-litigated one `fontSize: 13` at a time — which is how it reached twenty
// type sizes, three greens, and radii of 2/3/5/6/10/14/16px.
//
// Literals only: a computed value (`fontSize: size * 0.85`) is a relationship,
// not a magic number. Escape hatch is a line-level disable WITH a reason.
const designTokens = {
  rules: {
    "no-restricted-syntax": [
      "error",
      {
        selector: 'Property[key.name="fontSize"] > Literal[raw=/^[0-9.]+$/]',
        message:
          "Use a type-scale token: fontSize: \"var(--t-3)\". The scale is in app/globals.css; adding a 21st size is what it exists to prevent.",
      },
      {
        selector: 'Property[key.name="borderRadius"] > Literal[raw=/^[0-9.]+$/]',
        message:
          'Use a radius token: var(--r-1..4), or var(--r-full) for a pill. Pills are for controls you press, not for tags.',
      },
      {
        selector: "Literal[value=/^#[0-9a-fA-F]{6}$/]",
        message:
          "Use a colour token rather than a hex literal -- var(--success), var(--danger), var(--type-x402). See the token block in app/globals.css; lib/tokens.ts holds the two cases that genuinely need a raw hex.",
      },
    ],
  },
};

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    files: ["src/**/*.{ts,tsx}"],
    ...designTokens,
  },
  {
    // The one file whose job is to hold raw hex, and the tests that assert it
    // still matches the stylesheet.
    files: ["src/lib/tokens.ts", "src/lib/tokens.test.ts"],
    rules: { "no-restricted-syntax": "off" },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    // The native shell's static export (next.config.ts, MOBILE_BUILD) --
    // build output, same as out/ and .next/.
    "out-mobile/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
