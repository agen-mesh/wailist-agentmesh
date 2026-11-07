// @vitest-environment node
import { expect, it } from "vitest";
import { decodeWorkflowShare, gzipWorkflowShare } from "./workflowShare";
import { prepareSharePayload, publishedShareFields, shareURL, stripShareGraph } from "./sharePayload";

it("drops secrets and file bytes, and clears a template that could hold them", () => {
  const data = stripShareGraph({
    name: "Resume",
    nodes: [{
      id: "n1",
      type: "tool402",
      systemPrompt: "keep me",
      apiKey: "__enc__",
      emailApiKey: "mail-key",
      wallet: "ALGOADDR",
      secrets: { webhookSecret: "whsec", slackWebhookURL: "https://hooks.slack.com/x" },
      customParams: [
        { name: "resume", kind: "file", value: "QQQQ-FILE-BYTES", fileName: "a.pdf" },
        { name: "role", kind: "text", value: "engineer" },
      ],
      bodyTemplate: "prefix QQQQ-FILE-BYTES suffix",
    }],
    edges: [],
  });
  expect(data.nodes[0]).toMatchObject({
    systemPrompt: "keep me",
    bodyTemplate: "",
    customParams: [
      { name: "resume", kind: "file", value: "", fileName: "a.pdf" },
      { name: "role", kind: "text", value: "" },
    ],
  });
  expect(data.nodes[0]).not.toHaveProperty("apiKey");
  expect(data.nodes[0]).not.toHaveProperty("emailApiKey");
  expect(data.nodes[0]).not.toHaveProperty("secrets");
  expect(data.nodes[0]).not.toHaveProperty("wallet");
  expect(JSON.stringify(data)).not.toContain("QQQQ-FILE-BYTES");
  expect(JSON.stringify(data)).not.toContain("whsec");
});

it("refuses an incompressible gzip over 131072 bytes", async () => {
  // getRandomValues rejects a view longer than 65536 bytes.
  const raw = new Uint8Array(140_000);
  for (let i = 0; i < raw.length; i += 65536) {
    crypto.getRandomValues(raw.subarray(i, Math.min(i + 65536, raw.length)));
  }
  const systemPrompt = Buffer.from(raw).toString("latin1");
  await expect(prepareSharePayload({
    nodes: [{ id: "n1", type: "agent", systemPrompt }],
    edges: [],
  })).rejects.toThrow(
    "This workflow is over 128 KB after secrets and files are removed, so it cannot be shared.",
  );
});

it("round-trips a small graph through the existing decoder", async () => {
  const { bytes, data } = await prepareSharePayload({
    name: "Resume",
    nodes: [{ id: "n1", type: "agent", systemPrompt: "hi", apiKey: "sk-live" }],
    edges: [{ id: "e1", from: "n1", to: "n1", kind: "flow" }],
  });
  expect(bytes.length).toBeLessThanOrEqual(131072);
  expect(bytes[0]).toBe(0x1f);
  expect(bytes[1]).toBe(0x8b);
  const decoded = await decodeWorkflowShare(Buffer.from(bytes).toString("base64"));
  expect(decoded.name).toBe("Resume");
  expect(JSON.stringify(decoded)).not.toContain("sk-live");
  expect(decoded.nodes).toHaveLength(data.nodes.length);
});

it("blanks free-text values and lists the urls a public share will publish", () => {
  const data = stripShareGraph({
    nodes: [{
      id: "slack",
      type: "tool",
      url: "https://hooks.slack.com/services/T00/B00/secret",
      systemPrompt: "keep the prompt",
      wallet: "ALGOADDR",
      balance: "10",
      spent: "1",
      config: { token: "cfg-secret" },
      stateValue: "state-secret",
      paramDefaults: { apiKey: "param-secret" },
      customParams: [{ name: "token", kind: "text", value: "custom-secret" }],
      emailTo: "ops@example.com",
    }],
    edges: [],
  });
  const node = data.nodes[0] as Record<string, unknown>;
  expect(node.url).toBe("https://hooks.slack.com/services/T00/B00/secret");
  expect(node.systemPrompt).toBe("keep the prompt");
  expect(node.emailTo).toBe("ops@example.com");
  expect(node.paramDefaults).toEqual({ apiKey: "" });
  expect(node.customParams).toEqual([{ name: "token", kind: "text", value: "" }]);
  expect(node).not.toHaveProperty("wallet");
  expect(node).not.toHaveProperty("balance");
  expect(node).not.toHaveProperty("config");
  expect(node).not.toHaveProperty("stateValue");
  expect(JSON.stringify(data)).not.toContain("custom-secret");
  expect(JSON.stringify(data)).not.toContain("param-secret");
  expect(JSON.stringify(data)).not.toContain("cfg-secret");
  const published = publishedShareFields(data);
  expect(published.urls).toEqual(["https://hooks.slack.com/services/T00/B00/secret"]);
  expect(published.emails).toEqual(["ops@example.com"]);
});

it("stops a gzip that expands past 4 MB", async () => {
  const bytes = await gzipWorkflowShare({
    nodes: [{ id: "n1", type: "agent", systemPrompt: "a".repeat(4 * 1024 * 1024 + 1) }],
    edges: [],
  });
  expect(bytes.length).toBeLessThan(131072);
  await expect(decodeWorkflowShare(Buffer.from(bytes).toString("base64"))).rejects.toThrow(
    "This share is too large to open.",
  );
});

it("keeps the decryption key out of the path", () => {
  const url = shareURL("https://www.agent-mesh.app", "0x" + "ab".repeat(32), "KEYMATERIAL");
  expect(url).toBe(`https://www.agent-mesh.app/s/${"0x" + "ab".repeat(32)}#k=KEYMATERIAL`);
  expect(url.split("#")[0]).not.toContain("KEYMATERIAL");
  expect(shareURL("https://www.agent-mesh.app/", "0x" + "ab".repeat(32), null)).toBe(
    `https://www.agent-mesh.app/s/${"0x" + "ab".repeat(32)}`,
  );
});
