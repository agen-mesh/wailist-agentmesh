import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const gone =
  "This link has died. Shares last 7 days, and a retired Arkiv testnet deletes them. AgentMesh does not keep a copy.";

const mocks = vi.hoisted(() => ({
  openSharedWorkflow: vi.fn(),
  loadTemplateWorkflow: vi.fn(async (_workflow: { nodes: Array<Record<string, unknown>> }) => "wf_saved"),
  create: vi.fn(),
  signedIn: true,
  authLoading: false,
  readOnly: false,
}));

vi.mock("@/lib/arkivShare", () => {
  class ShareGoneError extends Error {
    constructor() {
      super(gone);
      this.name = "ShareGoneError";
    }
  }
  return { openSharedWorkflow: mocks.openSharedWorkflow, ShareGoneError };
});

vi.mock("@/lib/templateWorkflow", () => ({
  loadTemplateWorkflow: mocks.loadTemplateWorkflow,
}));

vi.mock("@/lib/api", () => ({
  workflows: { create: mocks.create, update: vi.fn() },
}));

vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("@/hooks/useAuth", () => ({
  useAuth: () => ({ signedIn: mocks.signedIn, loading: mocks.authLoading }),
}));

vi.mock("@/hooks/useReadOnly", () => ({
  useReadOnly: () => mocks.readOnly,
}));

import { ShareOpen } from "./ShareOpen";
import { generateStaticParams } from "@/app/s/[key]/page";

const graph = {
  name: "Resume",
  nodes: [{ id: "n1", type: "agent" }],
  edges: [{ id: "e1", from: "n1", to: "n1", kind: "flow" }],
};

afterEach(() => {
  cleanup();
  mocks.openSharedWorkflow.mockReset();
  mocks.loadTemplateWorkflow.mockClear();
  mocks.create.mockClear();
  mocks.signedIn = true;
  mocks.authLoading = false;
  mocks.readOnly = false;
  sessionStorage.clear();
});

it("shows the graph and saves only when asked", async () => {
  mocks.openSharedWorkflow.mockResolvedValue(graph);
  render(<ShareOpen entityKey={"0x" + "ab".repeat(32)} hash="" />);
  expect(await screen.findByText("Resume")).toBeTruthy();
  expect(screen.getByText("1 node")).toBeTruthy();
  expect(screen.getByText("1 edge")).toBeTruthy();
  expect(
    screen.getByText("Signed by AgentMesh. This link does not prove which person shared it."),
  ).toBeTruthy();
  expect(mocks.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save to my workflows" }));
  await waitFor(() => expect(mocks.loadTemplateWorkflow).toHaveBeenCalledTimes(1));
  expect(mocks.loadTemplateWorkflow).toHaveBeenCalledWith(
    expect.objectContaining({
      name: "Resume",
      nodes: graph.nodes,
      edges: graph.edges,
    }),
  );
  expect(mocks.create).not.toHaveBeenCalled();
});

it("shows a dead link without a save button", async () => {
  mocks.openSharedWorkflow.mockRejectedValue(new Error(gone));
  render(<ShareOpen entityKey={"0x" + "cd".repeat(32)} hash="" />);
  expect(await screen.findByText(gone)).toBeTruthy();
  expect(
    screen.queryByText("Signed by AgentMesh. This link does not prove which person shared it."),
  ).toBeNull();
  expect(screen.queryByRole("button", { name: "Save to my workflows" })).toBeNull();
  expect(mocks.create).not.toHaveBeenCalled();
});

it("keeps save available after a failed save", async () => {
  mocks.openSharedWorkflow.mockResolvedValue(graph);
  mocks.loadTemplateWorkflow.mockRejectedValueOnce(new Error("sign in"));
  render(<ShareOpen entityKey={"0x" + "ab".repeat(32)} hash="" />);
  fireEvent.click(await screen.findByRole("button", { name: "Save to my workflows" }));
  expect(await screen.findByText("sign in")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Save to my workflows" })).toBeTruthy();
  expect(
    screen.getByText("Signed by AgentMesh. This link does not prove which person shared it."),
  ).toBeTruthy();
});

it("strips secrets again before saving a hand-built share", async () => {
  mocks.openSharedWorkflow.mockResolvedValue({
    name: "Resume",
    nodes: [{ id: "n1", type: "tool", apiKey: "sk-live", url: "https://hooks.slack.com/x", secrets: { a: "b" } }],
    edges: [],
  });
  render(<ShareOpen entityKey={"0x" + "ab".repeat(32)} hash="" />);
  fireEvent.click(await screen.findByRole("button", { name: "Save to my workflows" }));
  await waitFor(() => expect(mocks.loadTemplateWorkflow).toHaveBeenCalledTimes(1));
  const saved = mocks.loadTemplateWorkflow.mock.calls[0][0];
  expect(saved.nodes[0].url).toBe("https://hooks.slack.com/x");
  expect(saved.nodes[0]).not.toHaveProperty("apiKey");
  expect(saved.nodes[0]).not.toHaveProperty("secrets");
});

it("offers sign-in and keeps the fragment key off the next path", async () => {
  mocks.signedIn = false;
  mocks.openSharedWorkflow.mockResolvedValue(graph);
  const entityKey = "0x" + "ab".repeat(32);
  render(<ShareOpen entityKey={entityKey} hash="#k=KEYMATERIAL" />);
  const link = await screen.findByRole("link", { name: "Sign in to save" });
  expect(link.getAttribute("href")).toBe(`/signin?next=${encodeURIComponent(`/s/${entityKey}`)}`);
  expect(link.getAttribute("href")).not.toContain("KEYMATERIAL");
  fireEvent.click(link);
  expect(sessionStorage.getItem(`arkiv-share-k:${entityKey}`)).toBe("KEYMATERIAL");
  expect(screen.queryByRole("button", { name: "Save to my workflows" })).toBeNull();
});

it("tells a phone viewer to open the link on a computer", async () => {
  mocks.readOnly = true;
  mocks.openSharedWorkflow.mockResolvedValue(graph);
  render(<ShareOpen entityKey={"0x" + "ab".repeat(32)} hash="" />);
  expect(await screen.findByText("Open this link on a computer to save it.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Save to my workflows" })).toBeNull();
  expect(screen.queryByRole("link", { name: "Sign in to save" })).toBeNull();
});

it("emits no static paths for the web build", () => {
  expect(process.env.MOBILE_BUILD).toBeUndefined();
  expect(generateStaticParams()).toEqual([]);
});
