import { describe, it, expect } from "vitest";
import { unified } from "unified";
import remarkParse from "remark-parse";
import remarkMath from "remark-math";
import remarkRehype from "remark-rehype";
import rehypeKatex from "rehype-katex";
import { toText } from "hast-util-to-text";
import type { Nodes } from "hast";
import { MATH_OPTIONS } from "./MarkdownContent";

async function visible(text: string): Promise<string> {
  const proc = unified()
    .use(remarkParse)
    .use(remarkMath, MATH_OPTIONS)
    .use(remarkRehype)
    .use(rehypeKatex);
  const tree = await proc.run(proc.parse(text));
  return toText(tree as Nodes)
    .replace(/\s+/g, " ")
    .trim();
}

describe("chat math options", () => {
  // Two dollar amounts on one line is the shape of every crypto report, and
  // with single-dollar math on, everything between them is parsed as an
  // expression: spaces collapse, the hyphen becomes a minus sign, and the
  // dollar signs disappear.
  it("leaves a price pair alone", async () => {
    const text = "BTC is $0.00001 USD, with a 24-hour volume of around $13.14.";
    expect(await visible(text)).toBe(text);
  });

  it("still renders explicit block math", async () => {
    const out = await visible("$$a + b$$");
    expect(out).toContain("a");
    expect(out).not.toContain("$$");
  });
});
