import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { renderToString } from "react-dom/server";

// Which screen a workflow opens as. The two screens are stubbed: what matters
// here is that a handheld never mounts the canvas, not what either one draws.
const state = vi.hoisted(() => ({
  handheld: false,
  canvas: vi.fn(),
}));

vi.mock("@/hooks/useIsHandheld", () => ({
  useIsHandheld: () => state.handheld,
}));
vi.mock("@/components/canvas/CanvasPage", () => ({
  CanvasPage: ({ workflowId }: { workflowId: string }) => {
    state.canvas(workflowId);
    return <div>canvas {workflowId}</div>;
  },
}));
vi.mock("./WorkflowSummary", () => ({
  WorkflowSummary: ({ workflowId }: { workflowId: string }) => (
    <div>summary {workflowId}</div>
  ),
}));

afterEach(() => {
  cleanup();
  state.handheld = false;
  state.canvas.mockClear();
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("WorkflowRoute", () => {
  it("opens the canvas on a desktop", async () => {
    const { WorkflowRoute } = await import("./WorkflowRoute");
    render(<WorkflowRoute workflowId="wf-1" />);
    expect(screen.getByText("canvas wf-1")).toBeTruthy();
    expect(screen.queryByText("summary wf-1")).toBeNull();
  });

  it("opens the summary on a handheld and never mounts the canvas", async () => {
    state.handheld = true;
    const { WorkflowRoute } = await import("./WorkflowRoute");
    render(<WorkflowRoute workflowId="wf-1" />);
    expect(screen.getByText("summary wf-1")).toBeTruthy();
    expect(state.canvas).not.toHaveBeenCalled();
  });

  it("always opens the summary in the native app", async () => {
    vi.stubEnv("NEXT_PUBLIC_NATIVE_CLIENT", "1");
    vi.resetModules();
    const { WorkflowRoute } = await import("./WorkflowRoute");
    // Even when the device check says desktop, as it does in this test
    // environment: the native app has no canvas to fall back to.
    render(<WorkflowRoute workflowId="wf-1" />);
    expect(screen.getByText("summary wf-1")).toBeTruthy();
    expect(state.canvas).not.toHaveBeenCalled();
  });

  it("renders neither screen on the server, where the device is unknown", async () => {
    const { WorkflowRoute } = await import("./WorkflowRoute");
    const html = renderToString(<WorkflowRoute workflowId="wf-1" />);
    expect(html).not.toContain("canvas");
    expect(html).not.toContain("summary");
    expect(state.canvas).not.toHaveBeenCalled();
  });
});
