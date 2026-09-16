import { describe, it, expect } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import { MATH_OPTIONS } from "./MarkdownContent";

// Only direct dependencies are imported here. The production build type-checks
// test files too, and it installs with pnpm, whose node_modules does not
// expose transitive packages -- an earlier version of this test imported
// remark-parse and hast-util-to-text, which resolved locally under npm and
// broke the deploy.
//
// The plugins are passed straight to react-markdown rather than through
// MarkdownContent, whose math plugins load lazily in an effect that a static
// render never runs.
function visible(text: string): string {
  const html = renderToStaticMarkup(
    createElement(
      ReactMarkdown,
      {
        remarkPlugins: [[remarkMath, MATH_OPTIONS]],
        rehypePlugins: [rehypeKatex],
      },
      text,
    ),
  );
  return html
    .replace(/<[^>]+>/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

describe("chat math options", () => {
  // Two dollar amounts on one line is the shape of every crypto report, and
  // with single-dollar math on, everything between them is parsed as an
  // expression: spaces collapse, the hyphen becomes a minus sign, and the
  // dollar signs disappear.
  it("leaves a price pair alone", () => {
    const text = "BTC is $0.00001 USD, with a 24-hour volume of around $13.14.";
    expect(visible(text)).toBe(text);
  });

  it("still renders explicit block math", () => {
    const out = visible("$$a + b$$");
    expect(out).toContain("a");
    expect(out).not.toContain("$$");
  });
});
