"use client";
import type { Element as HastElement } from "hast";
import ReactMarkdown, { type Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import "katex/dist/katex.min.css";
import { CodeBlock } from "./CodeBlock";

// Renders one message's text as markdown: headers/lists/tables/emphasis via
// remark-gfm, inline `$..$` and block `$$..$$` math via remark-math +
// rehype-katex, and fenced code blocks as CodeBlock (syntax highlight + copy
// button). Plain text with no markdown in it still round-trips fine -- it
// just comes out as a single <p>.
//
// `pre` is the only element we intercept. Reaching into its hast `node`
// (rather than trusting the already-rendered `children`, which by the time
// `pre` runs have already gone through react-markdown's own `code`
// component) is what lets one code path cover both `language-xxx` fences and
// unlabelled ones -- a fence with no language still arrives as `pre > code`,
// it just has no `language-` class on it.

/** Exported for tests: the raw-text and language extraction is what makes
 * both labelled and unlabelled fences render through the same CodeBlock. */
export function textOf(node: HastElement | undefined): string {
  if (!node) return "";
  return node.children
    .map((child) => (child.type === "text" ? child.value : textOf(child as HastElement)))
    .join("");
}

export function languageOf(node: HastElement): string | undefined {
  const raw = node.properties?.className;
  const classes = Array.isArray(raw) ? raw.map(String) : [];
  const match = classes.find((c) => c.startsWith("language-"));
  return match?.slice("language-".length);
}

const components: Components = {
  pre({ node, children }) {
    const codeNode = node?.children.find(
      (c): c is HastElement => c.type === "element" && c.tagName === "code",
    );
    if (!codeNode) return <pre>{children}</pre>;
    return <CodeBlock code={textOf(codeNode).replace(/\n$/, "")} language={languageOf(codeNode)} />;
  },
  // Wide tables scroll inside their own container rather than pushing the
  // whole ~62ch bubble (and the rail around it) wider.
  table({ children }) {
    return (
      <div className="chat-table-wrap">
        <table>{children}</table>
      </div>
    );
  },
};

export function MarkdownContent({ text }: { text: string }) {
  return (
    <div className="chat-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
