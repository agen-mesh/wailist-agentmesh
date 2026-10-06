// @vitest-environment node
//
// node, not the project's default jsdom: jsdom provides neither
// CompressionStream nor DecompressionStream, and vitest.config.ts has no
// setupFiles to shim them in. Node 18+ has both as globals, along with
// atob/btoa, so the codec runs here exactly as it does in a browser -- which
// is the point. Shimming jsdom instead would leave this file testing the shim.

import { describe, expect, it } from "vitest";
import type { NodeType, WorkflowEdge, WorkflowNode } from "./types";
import {
  SHAREABLE_NODE_TYPES,
  decodeWorkflowShare,
  encodeWorkflowShare,
  validateShareData,
} from "./workflowShare";

const node = (id: string, type: NodeType = "agent"): WorkflowNode =>
  ({ id, type, x: 0, y: 0 }) as WorkflowNode;

const edge = (id: string, from: string, to: string): WorkflowEdge =>
  ({ id, from, to, kind: "flow" }) as WorkflowEdge;

const graph = {
  name: "Resume screener",
  nodes: [node("n1", "trigger"), node("n2")],
  edges: [edge("e1", "n1", "n2")],
};

// A legacy code: the pre-versioning format, bare standard base64 of a gzipped
// payload with no `v` and no prefix. Built here the way the old encoder built
// it, so the compatibility test exercises the real shape rather than a
// re-description of it.
async function legacyCode(payload: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify(payload));
  const stream = new CompressionStream("gzip");
  const writer = stream.writable.getWriter();
  void writer.write(bytes);
  void writer.close();
  const compressed = new Uint8Array(
    await new Response(stream.readable).arrayBuffer(),
  );
  let binary = "";
  for (const b of compressed) binary += String.fromCharCode(b);
  return btoa(binary);
}

describe("encodeWorkflowShare / decodeWorkflowShare", () => {
  it("round-trips a workflow", async () => {
    const decoded = await decodeWorkflowShare(await encodeWorkflowShare(graph));
    expect(decoded.name).toBe("Resume screener");
    expect(decoded.nodes.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(decoded.edges).toHaveLength(1);
  });

  it("produces a code that survives a URL and a chat client", async () => {
    const code = await encodeWorkflowShare(graph);
    expect(code.startsWith("am1.")).toBe(true);
    // base64url: none of the three characters that get escaped in a URL or
    // mangled by clients that helpfully linkify text.
    expect(code.slice(4)).not.toMatch(/[+/=]/);
    expect(encodeURIComponent(code)).toBe(code);
  });

  it("carries the description, so a code says as much as a link", async () => {
    const decoded = await decodeWorkflowShare(
      await encodeWorkflowShare({ ...graph, description: "Screens CVs." }),
    );
    expect(decoded.description).toBe("Screens CVs.");
  });

  it("decodes a code that something wrapped on its way here", async () => {
    // A code is one long unbroken string, and the places it travels through
    // break long strings for a living: mail clients hard-wrap at 78 columns,
    // terminals wrap on paste, chat clients insert a newline mid-token.
    // Trimming only the ends left those breaks in the middle, where they
    // reached atob and threw -- so a perfectly good code came back as "that
    // doesn't look like a workflow code" purely because it had been emailed.
    const code = await encodeWorkflowShare(graph);
    const wrapped = (code.match(/.{1,40}/g) ?? []).join("\r\n");
    const spaced = ` ${code.slice(0, 20)} ${code.slice(20)}\t`;

    for (const mangled of [wrapped, spaced]) {
      const decoded = await decodeWorkflowShare(mangled);
      expect(decoded.name).toBe("Resume screener");
      expect(decoded.nodes).toHaveLength(2);
    }
  });

  it("still decodes a code made before the format was versioned", async () => {
    const old = await legacyCode({
      name: "From an older build",
      nodes: [node("n1", "trigger")],
      edges: [],
    });
    const decoded = await decodeWorkflowShare(old);
    expect(decoded.name).toBe("From an older build");
    expect(decoded.nodes).toHaveLength(1);
  });

  it("gzip actually earns its place", async () => {
    // The comment at the top of workflowShare.ts claims gzip-then-base64 beats
    // raw JSON despite base64's 33% overhead. On a graph with real repetition
    // that should hold; if it ever stops, the comment is wrong.
    const big = {
      name: "Wide",
      nodes: Array.from({ length: 40 }, (_, i) => node(`n${i}`, "tool402")),
      edges: Array.from({ length: 39 }, (_, i) =>
        edge(`e${i}`, `n${i}`, `n${i + 1}`),
      ),
    };
    const code = await encodeWorkflowShare(big);
    expect(code.length).toBeLessThan(JSON.stringify(big).length);
  });

  it("rejects a paste that is not a code at all", async () => {
    await expect(decodeWorkflowShare("hello there")).rejects.toThrow();
    await expect(decodeWorkflowShare("")).rejects.toThrow();
  });
});

describe("validateShareData", () => {
  it("accepts a well-formed graph", () => {
    expect(validateShareData({ v: 1, ...graph }).nodes).toHaveLength(2);
  });

  it("stays in step with the NodeType union", () => {
    // The allowlist is a runtime duplicate of a compile-time union, so nothing
    // but a test can notice the two drifting. This is the assignment that
    // fails to compile if a type is added to types.ts and not here.
    const every: Record<NodeType, true> = {
      trigger: true,
      agent: true,
      provider: true,
      tool: true,
      tool402: true,
      action: true,
      state: true,
      end: true,
      tendril: true,
      google: true,
    };
    expect([...SHAREABLE_NODE_TYPES].sort()).toEqual(Object.keys(every).sort());
  });

  it("refuses a node type the runner would not know what to do with", () => {
    expect(() =>
      validateShareData({ nodes: [{ id: "n1", type: "wat" }], edges: [] }),
    ).toThrow(/don't recognise/);
  });

  it("refuses a node with no id, and two nodes sharing one", () => {
    expect(() =>
      validateShareData({ nodes: [{ type: "agent" }], edges: [] }),
    ).toThrow(/no id/);
    expect(() =>
      validateShareData({ nodes: [node("n1"), node("n1")], edges: [] }),
    ).toThrow(/sharing an id/);
  });

  it("refuses an edge pointing at a node that is not in the payload", () => {
    // The one that matters most: a dangling edge reaches the topological sort
    // as a reference to a node that does not exist.
    expect(() =>
      validateShareData({
        nodes: [node("n1")],
        edges: [edge("e1", "n1", "ghost")],
      }),
    ).toThrow(/isn't in it/);
  });

  it("refuses an empty graph", () => {
    expect(() => validateShareData({ nodes: [], edges: [] })).toThrow(
      /no nodes/,
    );
  });

  it("refuses anything that is not a graph", () => {
    expect(() => validateShareData(null)).toThrow();
    expect(() => validateShareData("a string")).toThrow();
    expect(() => validateShareData({ nodes: "not an array" })).toThrow();
  });
});
