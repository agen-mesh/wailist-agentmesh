import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { WorkflowShareData } from "@/lib/workflowShare";

const api = vi.hoisted(() => ({
  read: vi.fn(),
  importInto: vi.fn(),
  importGraph: vi.fn(),
  decode: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  shares: { read: api.read, importInto: api.importInto },
  workflows: { importGraph: api.importGraph },
}));
vi.mock("@/lib/workflowShare", () => ({ decodeWorkflowShare: api.decode }));
vi.mock("@/hooks/useCloseOnBack", () => ({
  useCloseOnBack: () => () => {},
}));

import { ImportModal } from "./ImportModal";

const token = "abcdefghijklmnopqrstuv";
const graphName = "Inspect before importing";
const graph: WorkflowShareData = {
  name: graphName,
  description: "Uses your own connection.",
  nodes: [{ id: "start", type: "trigger", x: 0, y: 0 }],
  edges: [],
};
const response = {
  share: {
    token,
    name: graph.name,
    description: graph.description,
    graph: { nodes: graph.nodes, edges: graph.edges },
    nodeCount: 1,
    edgeCount: 0,
    createdAt: "2026-10-17T00:00:00Z",
  },
  requirements: { apiKeys: 0, files: 0, connectedAccounts: 1 },
};

beforeEach(() => {
  vi.resetAllMocks();
  api.read.mockResolvedValue(response);
  api.importInto.mockResolvedValue({ id: "copied-link" });
  api.importGraph.mockResolvedValue({ id: "copied-code" });
  api.decode.mockResolvedValue(graph);
});

afterEach(cleanup);

function openModal() {
  const onImported = vi.fn();
  const onClose = vi.fn();
  render(<ImportModal onImported={onImported} onClose={onClose} />);
  return { onImported, onClose };
}

function paste(value: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value } });
}

describe("ImportModal", () => {
  it("restores focus to the opener after the dialog unmounts", () => {
    const opener = document.createElement("button");
    opener.textContent = "Import a link";
    document.body.append(opener);
    opener.focus();
    try {
      const view = render(<ImportModal onImported={vi.fn()} onClose={vi.fn()} />);
      expect(document.activeElement).toBe(screen.getByRole("dialog", { name: "Import workflow" }));
      view.unmount();
      expect(document.activeElement).toBe(opener);
    } finally {
      opener.remove();
    }
  });

  it("returns focus to the preview when the async lookup blurs Continue", async () => {
    const lookup = Promise.withResolvers<typeof response>();
    api.read.mockReturnValueOnce(lookup.promise);
    openModal();
    paste(token);
    const continueButton = screen.getByRole("button", { name: "Continue" });
    continueButton.focus();
    fireEvent.click(continueButton);
    await waitFor(() => expect(continueButton.hasAttribute("disabled")).toBe(true));
    // This test DOM keeps disabled buttons focused; browsers blur them.
    document.body.setAttribute("tabindex", "-1");
    document.body.focus();
    document.body.removeAttribute("tabindex");
    expect(document.activeElement).toBe(document.body);

    lookup.resolve(response);
    const importButton = await screen.findByRole("button", { name: "Import to my workspace" });
    expect(document.activeElement).toBe(screen.getByRole("dialog", { name: "Import workflow" }));
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(importButton);
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
    expect(api.importInto).not.toHaveBeenCalled();
  });

  it("preserves focus on a dialog control while the preview finishes loading", async () => {
    const lookup = Promise.withResolvers<typeof response>();
    api.read.mockReturnValueOnce(lookup.promise);
    openModal();
    paste(token);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    const close = screen.getByRole("button", { name: "Close" });
    close.focus();
    lookup.resolve(response);
    await screen.findByRole("button", { name: "Import to my workspace" });
    expect(document.activeElement).toBe(close);
    expect(api.importInto).not.toHaveBeenCalled();
  });

  it.each(["link", "code"])("shows email setup before importing a sanitized %s", async (kind) => {
    const emailGraph: WorkflowShareData = {
      name: "Webhook email",
      nodes: [
        { id: "start", type: "trigger", template: "webhook", x: 0, y: 0 },
        { id: "email", type: "action", template: "email", emailProvider: "sendgrid", x: 100, y: 0 },
      ],
      edges: [{ id: "flow", from: "start", to: "email", kind: "flow" }],
    };
    api.read.mockResolvedValue({
      share: { ...response.share, name: emailGraph.name, graph: emailGraph, nodeCount: 2, edgeCount: 1 },
      requirements: { apiKeys: 0, files: 0, connectedAccounts: 0, emailActions: 1 },
    });
    api.decode.mockResolvedValue(emailGraph);
    openModal();
    paste(kind === "link" ? token : "am1.email-graph");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText("Your own email provider API key, for one email action");
    expect(screen.getByText("Your sender and recipient email addresses, for one email action")).toBeTruthy();
    expect(screen.queryByText("Ready to run as-is")).toBeNull();
    expect(screen.queryByText(/Nothing to configure/)).toBeNull();
    expect(api.importInto).not.toHaveBeenCalled();
    expect(api.importGraph).not.toHaveBeenCalled();
  });

  it("shows the link and required connection before creating a copy", async () => {
    const { onImported } = openModal();
    paste(`https://www.agent-mesh.app/s/${token}`);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText("Your own Google account, connected once");
    expect(screen.getByText(graphName)).toBeTruthy();
    expect(api.read).toHaveBeenCalledWith(token);
    expect(api.importInto).not.toHaveBeenCalled();
    expect(api.importGraph).not.toHaveBeenCalled();
    expect(onImported).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Import to my workspace" }));
    await waitFor(() => expect(onImported).toHaveBeenCalledWith("copied-link"));
    expect(api.importInto).toHaveBeenCalledExactlyOnceWith(token);
    expect(api.importGraph).not.toHaveBeenCalled();
  });

  it("imports a decoded code through the atomic graph endpoint after confirmation", async () => {
    const { onImported } = openModal();
    paste("am1.encoded-graph");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText("What it may still need");
    expect(api.read).not.toHaveBeenCalled();
    expect(api.importGraph).not.toHaveBeenCalled();
    expect(screen.queryByText("Ready to run as-is")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Import to my workspace" }));
    await waitFor(() => expect(onImported).toHaveBeenCalledWith("copied-code"));
    expect(api.importGraph).toHaveBeenCalledExactlyOnceWith(graph);
    expect(api.importInto).not.toHaveBeenCalled();
  });

  it("keeps an unavailable link out of the import confirmation", async () => {
    api.read.mockRejectedValue(new Error("this share link is no longer available"));
    const { onImported } = openModal();
    paste(`https://www.agent-mesh.app/s/${token}`);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText("this share link is no longer available");
    expect(api.decode).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Import to my workspace" })).toBeNull();
    expect(api.importInto).not.toHaveBeenCalled();
    expect(onImported).not.toHaveBeenCalled();
  });

  it("can retry when a link is revoked after its preview was loaded", async () => {
    api.importInto.mockRejectedValueOnce(new Error("this share link is no longer available"));
    const { onImported } = openModal();
    paste(token);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("button", { name: "Import to my workspace" }));

    await screen.findByText("this share link is no longer available");
    expect(onImported).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    paste("am1.replacement-graph");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.click(await screen.findByRole("button", { name: "Import to my workspace" }));
    await waitFor(() => expect(onImported).toHaveBeenCalledWith("copied-code"));
  });

  it("tries a bare legacy code after an ambiguous token lookup misses", async () => {
    api.read.mockRejectedValueOnce(new Error("not found"));
    openModal();
    paste(token);
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("What it may still need");
    expect(api.decode).toHaveBeenCalledExactlyOnceWith(token);
    expect(api.importGraph).not.toHaveBeenCalled();
  });

  it.each([{ ctrlKey: true }, { metaKey: true }])(
    "previews with the platform Enter shortcut without importing",
    async (modifier) => {
      openModal();
      paste(token);
      fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", ...modifier });
      await screen.findByText(graphName);
      expect(api.read).toHaveBeenCalledExactlyOnceWith(token);
      expect(api.importInto).not.toHaveBeenCalled();
    },
  );
});
