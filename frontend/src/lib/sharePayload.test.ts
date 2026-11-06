// @vitest-environment node
import { expect, it } from "vitest";
import { decodeWorkflowShare } from "./workflowShare";
import { prepareSharePayload, shareURL, stripShareGraph } from "./sharePayload";

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
    wallet: "ALGOADDR",
    bodyTemplate: "",
    customParams: [
      { name: "resume", kind: "file", value: "", fileName: "a.pdf" },
      { name: "role", kind: "text", value: "engineer" },
    ],
  });
  expect(data.nodes[0]).not.toHaveProperty("apiKey");
  expect(data.nodes[0]).not.toHaveProperty("emailApiKey");
  expect(data.nodes[0]).not.toHaveProperty("secrets");
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

it("keeps the decryption key out of the path", () => {
  const url = shareURL("https://www.agent-mesh.app", "0x" + "ab".repeat(32), "KEYMATERIAL");
  expect(url).toBe(`https://www.agent-mesh.app/s/${"0x" + "ab".repeat(32)}#k=KEYMATERIAL`);
  expect(url.split("#")[0]).not.toContain("KEYMATERIAL");
  expect(shareURL("https://www.agent-mesh.app/", "0x" + "ab".repeat(32), null)).toBe(
    `https://www.agent-mesh.app/s/${"0x" + "ab".repeat(32)}`,
  );
});
