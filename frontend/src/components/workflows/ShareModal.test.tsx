import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";

// The dialog is tested against a stubbed API, the way WorkflowsPage is. What
// matters here is which calls it makes and what it says about them, not the
// network.
const state = vi.hoisted(() => ({
  create: vi.fn(),
  listFor: vi.fn(),
  revoke: vi.fn(),
  read: vi.fn(),
  encode: vi.fn(),
  native: false,
}));

vi.mock("@/lib/api", () => ({
  shares: {
    create: state.create,
    listFor: state.listFor,
    revoke: state.revoke,
    read: state.read,
  },
}));
vi.mock("@/hooks/useModalDismissal", () => ({
  // Hands back a ref, the way the real hook does -- the dialog puts it on
  // its panel.
  useModalDismissal: () => ({ current: null }),
}));

vi.mock("@/lib/nativeAuth", () => ({
  get IS_NATIVE() {
    return state.native;
  },
}));
vi.mock("@/lib/workflowShare", () => ({
  encodeWorkflowShare: state.encode,
}));

import { ShareModal } from "./ShareModal";

const SHARE = {
  token: "tok-current",
  name: "Resume screener",
  graph: { nodes: [], edges: [] },
  nodeCount: 4,
  edgeCount: 3,
  importCount: 0,
  createdAt: new Date().toISOString(),
};

const REDACTIONS = {
  apiKeys: 2,
  secrets: 1,
  webhookSecrets: 1,
  uploadedFiles: 0,
  agentWallets: 0,
  emailAddresses: 0,
  connectedAccounts: 0,
  leasedMachines: 0,
};

beforeEach(() => {
  state.native = false;
  state.read.mockResolvedValue({ share: SHARE });
  state.encode.mockResolvedValue("amwf:sanitized-code");
  state.create.mockResolvedValue({ share: SHARE, redactions: REDACTIONS });
  state.listFor.mockResolvedValue([SHARE]);
  state.revoke.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(document, "execCommand");
});

describe("ShareModal", () => {
  it("asks for a link that matches the workflow as it is now", async () => {
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    await screen.findByText(/Not included:/);

    // reuseIfUnchanged, rather than the dialog picking a live link itself:
    // only the server holds the sanitised graph, so only it can tell whether
    // an existing link still describes this workflow. Choosing here is how a
    // link to an OLDER version used to get handed back.
    expect(state.create).toHaveBeenCalledWith("wf-1", 0, true);
  });

  it("says what was left out even when the link is one it already had", async () => {
    // The regression this exists for. The dialog used to reuse a live link by
    // itself, and that branch had no redaction counts to set -- so the
    // summary went silent at the one moment the sharer could still change
    // their mind about handing the link over.
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);

    const summary = await screen.findByText(/2 API keys/);
    expect(summary.textContent).toMatch(/webhook secret/);
  });

  it("keeps every other link behind one line until asked for", async () => {
    // Collapsed for the reason the Credits screen collapses "How credits
    // work": it costs one line rather than a field. The list grows without
    // bound, and it sat permanently on screen competing with Copy link.
    state.listFor.mockResolvedValue([SHARE, { ...SHARE, token: "tok-older" }]);

    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    const options = await screen.findByRole("button", { name: /Link options/ });

    expect(options.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText(/Other links to this workflow/)).toBeNull();
    // The count rides on the label, so the line is worth reading while shut.
    expect(options.textContent).toMatch(/1 other/);

    fireEvent.click(options);
    await screen.findByText(/Other links to this workflow/);

    // Two links exist, but the one shown above is not repeated underneath --
    // listed twice it reads as two different links. Tokens render as their
    // last 8 characters.
    expect(await screen.findByText(/ok-older/)).toBeTruthy();
    expect(screen.queryAllByText(/k-current/)).toHaveLength(0);
  });

  it("still shows a link that has been revoked", async () => {
    // The backend returns revoked and expired links deliberately, and this
    // view used to filter them out -- throwing away the only record of what
    // had been handed out and then pulled back. "I revoked that one" is the
    // answer to "your link is dead".
    state.listFor.mockResolvedValue([
      SHARE,
      { ...SHARE, token: "tok-dead", revokedAt: new Date().toISOString() },
    ]);

    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Link options/ }),
    );
    const row = await screen.findByText(/ok-dead/);
    expect(row.textContent).toMatch(/Revoked/);
  });

  it("will not offer to mint a duplicate of the link on screen", async () => {
    // "No expiry" used to be the default beside "Make it", so the obvious
    // press produced a SECOND never-expiring link to the same workflow --
    // against an allowance counted per user, for no benefit. It is offered
    // only when the current link does expire, so there is no state in which
    // this control makes something pointless.
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Link options/ }),
    );

    const offered = within(screen.getByLabelText(/Make another, expiring in/))
      .getAllByRole("radio")
      .map((r) => r.textContent);
    expect(offered).not.toContain("No expiry");
    expect(offered).toContain("7 days");
  });

  it("offers no expiry again once the current link has one", async () => {
    state.create.mockResolvedValue({
      share: {
        ...SHARE,
        expiresAt: new Date(Date.now() + 864e5).toISOString(),
      },
      redactions: REDACTIONS,
    });

    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Link options/ }),
    );

    expect(
      within(screen.getByLabelText(/Make another, expiring in/))
        .getAllByRole("radio")
        .map((r) => r.textContent),
    ).toContain("No expiry");
  });

  it("moves the expiry selection to the segment that was pressed", async () => {
    // The <select> this replaced could not get this wrong -- a native control
    // owns its own value. A set of buttons does not, so the thing worth
    // asserting is that pressing one both checks it AND unchecks the other:
    // two segments reading as chosen is the failure mode here.
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Link options/ }),
    );

    const group = screen.getByLabelText(/Make another, expiring in/);
    const checked = () =>
      within(group)
        .getAllByRole("radio")
        .filter((r) => r.getAttribute("aria-checked") === "true")
        .map((r) => r.textContent);

    expect(checked()).toEqual(["7 days"]);

    fireEvent.click(within(group).getByRole("radio", { name: "30 days" }));
    expect(checked()).toEqual(["30 days"]);
  });

  it("asks before revoking, because a link cannot be un-revoked", async () => {
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    const revoke = await screen.findByRole("button", { name: "Revoke" });

    fireEvent.click(revoke);
    // The first press only arms it.
    expect(state.revoke).not.toHaveBeenCalled();
    const armed = await screen.findByRole("button", { name: "Revoke?" });

    fireEvent.click(armed);
    expect(state.revoke).toHaveBeenCalledWith("tok-current");
  });
});


describe("ShareModal clipboard", () => {
  function clipboard(copyAccepted: boolean, native: boolean) {
    state.native = native;
    const writeText = vi.fn().mockRejectedValue(new Error("Write permission denied"));
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const copied: string[] = [];
    const execCommand = vi.fn(() => {
      const field = document.activeElement;
      if (copyAccepted && field instanceof HTMLTextAreaElement) {
        copied.push(field.value);
      }
      return copyAccepted;
    });
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: execCommand,
    });
    return { writeText, execCommand, copied };
  }

  it("copies the canonical native link and restores the Share control focus", async () => {
    const copy = clipboard(true, true);
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    const button = await screen.findByRole("button", { name: "Copy link" });
    button.focus();
    fireEvent.click(button);

    await screen.findByRole("button", { name: "Copied" });
    expect(copy.copied).toEqual(["https://www.agent-mesh.app/s/tok-current"]);
    expect(copy.writeText).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(button);
    expect(screen.getByLabelText("Share link")).toHaveProperty("readOnly", true);
  });

  it("copies only the encoded sanitized server snapshot on native", async () => {
    const copy = clipboard(true, true);
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    const button = await screen.findByRole("button", { name: "copy a code" });
    button.focus();
    fireEvent.click(button);

    await screen.findByText("Code copied to clipboard");
    expect(state.read).toHaveBeenCalledWith(SHARE.token);
    expect(state.encode).toHaveBeenCalledWith({
      name: SHARE.name,
      description: undefined,
      nodes: SHARE.graph.nodes,
      edges: SHARE.graph.edges,
    });
    expect(copy.copied).toEqual(["amwf:sanitized-code"]);
    expect(copy.writeText).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(button);
  });

  it("retains normal web copying without a temporary selection", async () => {
    const copy = clipboard(true, false);
    copy.writeText.mockResolvedValue(undefined);
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Copy link" }));

    await screen.findByRole("button", { name: "Copied" });
    expect(copy.writeText).toHaveBeenCalledWith(
      `${window.location.origin}/s/tok-current`,
    );
    expect(copy.execCommand).not.toHaveBeenCalled();
  });

  it("keeps manual link selection and reports code failure without claiming a copy", async () => {
    const copy = clipboard(false, true);
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Copy link" }));
    await waitFor(() => expect(copy.writeText).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("button", { name: "Copied" })).toBeNull();
    const link = screen.getByLabelText("Share link");
    expect(link).toHaveProperty("value", "https://www.agent-mesh.app/s/tok-current");

    fireEvent.click(screen.getByRole("button", { name: "copy a code" }));
    await screen.findByText("Clipboard access was blocked.");
    expect(screen.queryByText("Code copied to clipboard")).toBeNull();
  });

  it("keeps a revoked link uncopyable", async () => {
    const copy = clipboard(true, true);
    state.create.mockResolvedValue({
      share: { ...SHARE, revokedAt: new Date().toISOString() },
      redactions: REDACTIONS,
    });
    render(<ShareModal workflowId="wf-1" onClose={() => {}} />);
    const button = await screen.findByRole("button", { name: "Link is dead" });
    fireEvent.click(button);
    expect(button).toHaveProperty("disabled", true);
    expect(copy.execCommand).not.toHaveBeenCalled();
    expect(copy.writeText).not.toHaveBeenCalled();
  });
});
