import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const gone =
  "This link has died. Shares last 7 days, and a retired Arkiv testnet deletes them. AgentMesh does not keep a copy.";

const mocks = vi.hoisted(() => ({
  openSharedWorkflow: vi.fn(),
  loadTemplateWorkflow: vi.fn(async () => "wf_saved"),
  create: vi.fn(),
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

import { ShareOpen } from "./ShareOpen";
import { generateStaticParams } from "@/app/s/[key]/page";

const graph = {
  name: "Resume",
  nodes: [{ id: "n1", type: "agent" }],
  edges: [{ id: "e1", source: "n1", target: "n1" }],
};

afterEach(() => {
  cleanup();
  mocks.openSharedWorkflow.mockReset();
  mocks.loadTemplateWorkflow.mockClear();
  mocks.create.mockClear();
});

it("shows the graph and saves only when asked", async () => {
  mocks.openSharedWorkflow.mockResolvedValue(graph);
  render(<ShareOpen entityKey={"0x" + "ab".repeat(32)} hash="" />);
  expect(await screen.findByText("Resume")).toBeTruthy();
  expect(screen.getByText("1 nodes")).toBeTruthy();
  expect(screen.getByText("1 edges")).toBeTruthy();
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

it("emits no static paths for the web build", () => {
  expect(process.env.MOBILE_BUILD).toBeUndefined();
  expect(generateStaticParams()).toEqual([]);
});
