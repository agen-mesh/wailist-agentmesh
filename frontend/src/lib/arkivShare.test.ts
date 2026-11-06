// @vitest-environment node
import { beforeEach, expect, it, vi } from "vitest";
import { prepareSharePayload } from "./sharePayload";
import { openShare } from "./shareCrypto";

const mocks = vi.hoisted(() => ({
  getEntity: vi.fn(),
  createPublicClient: vi.fn(),
  openShare: null as null | ((ciphertext: Uint8Array, key: string) => Promise<Uint8Array>),
}));

vi.mock("@arkiv-network/sdk", () => ({
  createPublicClient: (...args: unknown[]) => {
    mocks.createPublicClient(...args);
    return { getEntity: mocks.getEntity };
  },
}));

vi.mock("@arkiv-network/sdk/chains", () => ({
  rabdi: (options: unknown) => ({ id: 7794095, options }),
}));

vi.mock("viem", () => ({
  http: () => ({ type: "http" }),
}));

vi.mock("./shareCrypto", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./shareCrypto")>();
  mocks.openShare = actual.openShare;
  return { ...actual, openShare: vi.fn(actual.openShare) };
});

import { ShareGoneError, openSharedWorkflow } from "./arkivShare";

const entityKey = "0x" + "ab".repeat(32);
const gone =
  "This link has died. Shares last 7 days, and a retired Arkiv testnet deletes them. AgentMesh does not keep a copy.";

let gzip: Uint8Array;

beforeEach(async () => {
  mocks.getEntity.mockReset();
  mocks.createPublicClient.mockReset();
  vi.mocked(openShare).mockReset();
  vi.mocked(openShare).mockImplementation((ciphertext, key) => mocks.openShare!(ciphertext, key));
  const prepared = await prepareSharePayload({
    name: "Resume",
    nodes: [{ id: "n1", type: "agent", systemPrompt: "hi" }],
    edges: [],
  });
  gzip = prepared.bytes;
});

it("reads the payload attribute and does not pass the fragment", async () => {
  mocks.getEntity.mockImplementation(async (key: string) => {
    expect(key).toBe(entityKey);
    expect(key.includes("#")).toBe(false);
    expect(key.includes("KEYMATERIAL")).toBe(false);
    return { attributes: { payload: { type: "bytes", value: gzip } } };
  });
  const data = await openSharedWorkflow(entityKey, "");
  expect(mocks.getEntity).toHaveBeenCalledOnce();
  expect(data.name).toBe("Resume");
  const clientArgs = JSON.stringify(mocks.createPublicClient.mock.calls);
  expect(clientArgs).not.toContain("privateKey");
  expect(clientArgs).not.toContain("ARKIV_SHARE_PRIVATE_KEY");
});

it("reads the SDK attribute map when the payload is hex", async () => {
  const hex = "0x" + Buffer.from(gzip).toString("hex");
  mocks.getEntity.mockResolvedValue({
    $attributes: { payload: { type: "bytes", value: hex } },
  });
  const data = await openSharedWorkflow(entityKey, "");
  expect(data.name).toBe("Resume");
});

it("uses the fragment key only for decrypt", async () => {
  mocks.getEntity.mockResolvedValue({
    attributes: { payload: { type: "bytes", value: gzip } },
  });
  vi.mocked(openShare).mockResolvedValue(gzip);
  await openSharedWorkflow(entityKey, "#k=KEYMATERIAL");
  expect(openShare).toHaveBeenCalledWith(expect.any(Uint8Array), "KEYMATERIAL");
  expect(mocks.getEntity.mock.calls[0][0]).not.toContain("KEYMATERIAL");
});

it("throws ShareGoneError when the entity or the payload is missing", async () => {
  mocks.getEntity.mockRejectedValue(new Error("missing"));
  await expect(openSharedWorkflow(entityKey, "")).rejects.toThrow(gone);
  await expect(openSharedWorkflow(entityKey, "")).rejects.toBeInstanceOf(ShareGoneError);

  mocks.getEntity.mockResolvedValue({ attributes: {} });
  await expect(openSharedWorkflow(entityKey, "")).rejects.toThrow(gone);
});

it("surfaces a bad fragment key and a public payload that is not gzip", async () => {
  mocks.getEntity.mockResolvedValue({
    attributes: { payload: { type: "bytes", value: gzip } },
  });
  await expect(openSharedWorkflow(entityKey, "#k=WRONG")).rejects.toThrow(
    "This private share needs the #k= key from the original link.",
  );

  mocks.getEntity.mockResolvedValue({
    attributes: { payload: { type: "bytes", value: new Uint8Array([1, 2, 3]) } },
  });
  await expect(openSharedWorkflow(entityKey, "")).rejects.toBeInstanceOf(ShareGoneError);
});
