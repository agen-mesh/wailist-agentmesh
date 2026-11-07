import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ShareModal } from "./ShareModal";
import { createShare } from "@/lib/shares";
import { sealShare } from "@/lib/shareCrypto";
import { prepareSharePayload } from "@/lib/sharePayload";
import { loadTemplateWorkflow } from "@/lib/templateWorkflow";

vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: true, authHeaders: () => ({}) }));

const readOnlyState = vi.hoisted(() => ({ current: false }));

vi.mock("@/hooks/useReadOnly", () => ({
  useReadOnly: () => readOnlyState.current,
}));

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    workflows: {
      ...actual.workflows,
      get: vi.fn(async () => ({
        id: "wf_1",
        name: "Resume",
        nodes: [{ id: "n1", type: "agent", apiKey: "sk-live", systemPrompt: "hi" }],
        edges: [],
      })),
    },
  };
});

vi.mock("@/lib/shares", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/shares")>();
  return { ...actual, createShare: vi.fn() };
});

vi.mock("@/lib/shareCrypto", () => ({ sealShare: vi.fn() }));

vi.mock("@/lib/sharePayload", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/sharePayload")>();
  return { ...actual, prepareSharePayload: vi.fn() };
});

vi.mock("@/lib/templateWorkflow", () => ({ loadTemplateWorkflow: vi.fn() }));

const tooLarge =
  "This workflow is over 128 KB after secrets and files are removed, so it cannot be shared.";
const authorship = "Signed by AgentMesh. This link does not prove which person shared it.";
const death =
  "This link dies 7 days after it is confirmed. AgentMesh does not keep a copy. A retired Arkiv testnet kills the link too.";
const unconfirmed = "Share did not confirm. Nothing was copied. Try again.";
const key = "0x" + "ab".repeat(32);

beforeEach(() => {
  vi.mocked(prepareSharePayload).mockResolvedValue({
    bytes: Uint8Array.of(1, 2, 3),
    data: { name: "Resume", nodes: [], edges: [] },
  });
  vi.mocked(sealShare).mockResolvedValue({
    ciphertext: new Uint8Array([7, 7, 7]),
    key: "KEYMATERIAL",
  });
  vi.mocked(createShare).mockResolvedValue({ entityKey: key, chainId: 7794095 });
  vi.mocked(loadTemplateWorkflow).mockReset();
});

afterEach(() => {
  cleanup();
  readOnlyState.current = false;
});

it("posts ciphertext and hides the key and the link until 201", async () => {
  const posted: Uint8Array[] = [];
  vi.mocked(createShare).mockImplementation(async (bytes) => {
    posted.push(bytes);
    expect(screen.queryByText(/0xab/)).toBeNull();
    return { entityKey: "0x" + "ab".repeat(32), chainId: 7794095 };
  });
  vi.mocked(sealShare).mockResolvedValue({
    ciphertext: new Uint8Array([7, 7, 7]),
    key: "KEYMATERIAL",
  });
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  expect(screen.getByText(authorship)).toBeTruthy();
  expect(screen.getByText(death)).toBeTruthy();
  expect(vi.mocked(createShare)).not.toHaveBeenCalled();
  const box = await screen.findByRole("checkbox", { name: "Anyone with the link can read this" });
  expect((box as HTMLInputElement).checked).toBe(false);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  const link = await screen.findByText(
    `https://www.agent-mesh.app/s/${"0x" + "ab".repeat(32)}#k=KEYMATERIAL`,
  );
  expect(posted).toEqual([new Uint8Array([7, 7, 7])]);
  expect(new TextDecoder().decode(posted[0])).not.toContain("KEYMATERIAL");
  expect(link.textContent?.split("#")[0]).not.toContain("KEYMATERIAL");
  expect(vi.mocked(loadTemplateWorkflow)).not.toHaveBeenCalled();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(screen.queryByRole("link", { name: "Post" })).toBeNull();
  expect(screen.queryByRole("link", { name: "Email" })).toBeNull();
  expect(screen.getByRole("button", { name: "Copy link" })).toBeTruthy();
});

it("posts the gzip when anyone with the link may read it", async () => {
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("checkbox", { name: "Anyone with the link can read this" }));
  expect(
    screen.getByText(
      "Public. Anyone who can query Arkiv can read this graph for 7 days, even without the link. Prompts are included.",
    ),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Create link" }));
  await screen.findByText(`https://www.agent-mesh.app/s/${key}`);
  expect(vi.mocked(sealShare)).not.toHaveBeenCalled();
  expect(vi.mocked(createShare)).toHaveBeenCalledWith(Uint8Array.of(1, 2, 3));
  expect(vi.mocked(loadTemplateWorkflow)).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "Post" })).toBeTruthy();
  expect(screen.getByRole("link", { name: "Email" })).toBeTruthy();
});

it("lists the urls and emails before a public share is confirmed", async () => {
  vi.mocked(prepareSharePayload).mockResolvedValue({
    bytes: Uint8Array.of(1, 2, 3),
    data: {
      name: "Resume",
      nodes: [{
        url: "https://hooks.slack.com/services/T00/B00/secret",
        emailTo: "ops@example.com",
      }],
      edges: [],
    },
  });
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("checkbox", { name: "Anyone with the link can read this" }));
  expect(
    screen.getByText(
      "This public share will publish https://hooks.slack.com/services/T00/B00/secret, ops@example.com.",
    ),
  ).toBeTruthy();
});

it("does not offer Create link on a viewer", async () => {
  readOnlyState.current = true;
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  expect(await screen.findByText("Workflows can only be edited in the AgentMesh desktop app.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Create link" })).toBeNull();
  expect(vi.mocked(createShare)).not.toHaveBeenCalled();
});

it("shows the hourly limit instead of asking for another try that cannot succeed", async () => {
  vi.mocked(createShare).mockRejectedValue(new Error("share_rate"));
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  expect(
    await screen.findByText(
      "You have reached the hourly share limit. Nothing was copied. Try again later.",
    ),
  ).toBeTruthy();
  expect(screen.queryByText(unconfirmed)).toBeNull();
});

it("shows that sharing is off when the server refuses the key", async () => {
  vi.mocked(createShare).mockRejectedValue(new Error("share_disabled"));
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  expect(await screen.findByText("Sharing is off on this server. Nothing was copied.")).toBeTruthy();
});

it("shows the unconfirmed sentence and no link when the post fails", async () => {
  vi.mocked(createShare).mockRejectedValue(new Error("nope"));
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  expect(await screen.findByText(unconfirmed)).toBeTruthy();
  expect(document.body.textContent).not.toContain("/s/0x");
});

it("does not render a link when the receipt is not Rabdi", async () => {
  vi.mocked(createShare).mockResolvedValue({ entityKey: key, chainId: 1 });
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  await screen.findByText(unconfirmed);
  expect(screen.queryByText(new RegExp(`/s/${key}`))).toBeNull();
});

it("refuses a sealed payload over 128 KB without posting", async () => {
  vi.mocked(sealShare).mockResolvedValue({
    ciphertext: new Uint8Array(131073),
    key: "KEYMATERIAL",
  });
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Create link" }));
  expect(await screen.findByText(tooLarge)).toBeTruthy();
  expect(vi.mocked(createShare)).not.toHaveBeenCalled();
});

it("disables Create link when the gzip is already over 128 KB", async () => {
  vi.mocked(prepareSharePayload).mockRejectedValue(new Error(tooLarge));
  render(<ShareModal workflowId="wf_1" onClose={() => {}} />);
  expect(await screen.findByText(tooLarge)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Create link" }) as HTMLButtonElement).disabled).toBe(true);
  expect(vi.mocked(createShare)).not.toHaveBeenCalled();
});
