import { afterEach, assert, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Dispatch, SetStateAction } from "react";
import type { Workflow } from "@/lib/types";

const mocks = vi.hoisted(() => ({
  get: vi.fn(), update: vi.fn(), estimate: vi.fn(),
  createShare: vi.fn(), listShares: vi.fn(), refreshBalance: vi.fn(),
  readOnly: false,
  router: { push: vi.fn(), replace: vi.fn() },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => mocks.router,
  useSearchParams: () => new URLSearchParams(),
}));
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  workflows: { get: mocks.get, update: mocks.update, estimate: mocks.estimate },
  shares: { create: mocks.createShare, listFor: mocks.listShares },
  runs: {},
}));
vi.mock("@/lib/credits/store", () => ({
  useCredits: () => ({
    balanceUSD: 10, balanceKnown: true, lowBalanceThresholdUSD: 1,
    refreshBalance: mocks.refreshBalance,
  }),
  refreshBalance: mocks.refreshBalance,
}));
vi.mock("@/hooks/useIsCompact", () => ({ useIsCompact: () => false }));
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => false }));
vi.mock("@/hooks/useReadOnly", () => ({ useReadOnly: () => mocks.readOnly }));
vi.mock("./PalettePanel", () => ({ PalettePanel: () => null }));
vi.mock("./Inspector", () => ({ Inspector: () => null }));
vi.mock("./ConsolePanel", () => ({ ConsolePanel: () => null }));
vi.mock("./ResizeHandle", () => ({ ResizeHandle: () => null }));
vi.mock("./chat/ChatRail", () => ({ ChatRail: () => null }));
vi.mock("./chat/useChatConsole", () => ({
  useChatConsole: () => ({ logs: [], elapsed: 0, done: false, deadLetters: [], session: {}, busy: false }),
}));
vi.mock("./CanvasGraph", () => ({
  CanvasGraph: ({ workflow, setWorkflow }: {
    workflow: Workflow;
    setWorkflow: Dispatch<SetStateAction<Workflow>>;
  }) => (
    <>
      <button onClick={() => setWorkflow((wf) => ({
        ...wf,
        nodes: [...wf.nodes, { id: `step-${wf.nodes.length}`, type: "agent", x: 100, y: 100 }],
        edges: [...wf.edges, { id: `edge-${wf.nodes.length}`, from: "trigger", to: `step-${wf.nodes.length}`, kind: "flow" }],
      }))}>Edit graph</button>
      <output data-testid="graph">{JSON.stringify({ nodes: workflow.nodes, edges: workflow.edges })}</output>
    </>
  ),
}));

import { CanvasPage } from "./CanvasPage";

const workflow: Workflow = {
  id: "workflow-share", name: "Save before Share", status: "draft",
  nodes: [{ id: "trigger", type: "trigger", x: 0, y: 0 }], edges: [],
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.readOnly = false;
  mocks.get.mockResolvedValue(workflow);
  mocks.update.mockResolvedValue(workflow);
  mocks.estimate.mockResolvedValue(null);
  mocks.createShare.mockResolvedValue({
    share: {
      token: "k3Jm9xQpLr2sTvWyZa1b", name: workflow.name,
      graph: { nodes: workflow.nodes, edges: workflow.edges },
      nodeCount: 1, edgeCount: 0, importCount: 0,
      createdAt: "2026-10-17T00:00:00Z",
    },
    redactions: {},
  });
  mocks.listShares.mockResolvedValue([]);
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("CanvasPage Share save boundary", () => {
  it("creates one snapshot only after the newest graph has saved", async () => {
    const saved = deferred<Workflow>();
    mocks.update.mockReturnValue(saved.promise);
    render(<CanvasPage workflowId={workflow.id} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit graph" }));
    const share = screen.getByRole("button", { name: "Share" });

    act(() => {
      share.click();
      share.click();
    });
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith(workflow.id, expect.objectContaining({
      nodes: expect.arrayContaining([expect.objectContaining({ id: "step-1" })]),
      edges: expect.arrayContaining([expect.objectContaining({ id: "edge-1" })]),
    }));
    expect((screen.getByRole("button", { name: "Saving…" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.createShare).not.toHaveBeenCalled();

    await act(async () => { saved.resolve(workflow); });

    expect(await screen.findByRole("dialog", { name: "Share workflow" })).toBeTruthy();
    expect(mocks.createShare).toHaveBeenCalledExactlyOnceWith(workflow.id, 0, true);
    const [saveOrder] = mocks.update.mock.invocationCallOrder;
    const [shareOrder] = mocks.createShare.mock.invocationCallOrder;
    assert.isDefined(saveOrder);
    assert.isDefined(shareOrder);
    expect(saveOrder).toBeLessThan(shareOrder);
  });

  it("keeps Share closed after a failed save and allows a successful edit retry", async () => {
    mocks.update.mockRejectedValueOnce(new Error("Save unavailable"));
    render(<CanvasPage workflowId={workflow.id} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit graph" }));
    const graph = screen.getByTestId("graph").textContent;
    fireEvent.click(screen.getByRole("button", { name: "Share" }));

    expect(await screen.findByText("Share cancelled: latest changes failed to save")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.createShare).not.toHaveBeenCalled();
    expect(screen.getByTestId("graph").textContent).toBe(graph);
    expect((screen.getByRole("button", { name: "Share" }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Edit graph" }));
    fireEvent.click(screen.getByRole("button", { name: "Share" }));

    expect(await screen.findByRole("dialog", { name: "Share workflow" })).toBeTruthy();
    expect(mocks.update).toHaveBeenCalledTimes(2);
    expect(mocks.createShare).toHaveBeenCalledTimes(1);
  });

  it("does not forget a failed flush when Share is pressed again without an edit", async () => {
    mocks.update.mockRejectedValueOnce(new Error("Save unavailable"));
    render(<CanvasPage workflowId={workflow.id} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit graph" }));
    fireEvent.click(screen.getByRole("button", { name: "Share" }));
    await screen.findByText("Share cancelled: latest changes failed to save");

    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Share" })); });

    expect(mocks.update).toHaveBeenCalledTimes(1);
    expect(mocks.createShare).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("handles a rejected flush without mounting the snapshot dialog", async () => {
    mocks.update.mockImplementationOnce(() => { throw new Error("Client unavailable"); });
    render(<CanvasPage workflowId={workflow.id} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit graph" }));
    fireEvent.click(screen.getByRole("button", { name: "Share" }));

    await screen.findByText("Share cancelled: latest changes failed to save");
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Share" })); });
    expect(mocks.createShare).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect((screen.getByRole("button", { name: "Share" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("allows read-only sharing independently of deployment permission", async () => {
    mocks.readOnly = true;
    render(<CanvasPage workflowId={workflow.id} />);
    fireEvent.click(await screen.findByRole("button", { name: "Share" }));

    expect(await screen.findByRole("dialog", { name: "Share workflow" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Deploy$|^Re-deploy$/ })).toBeNull();
    expect(mocks.update).not.toHaveBeenCalled();
    expect(mocks.createShare).toHaveBeenCalledTimes(1);
  });
});
