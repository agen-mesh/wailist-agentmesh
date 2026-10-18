import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ShareImportRequirements, WorkflowNode } from "@/lib/types";
import { SharedWorkflowCard } from "./SharedWorkflowCard";

afterEach(cleanup);

const emptyRequirements: ShareImportRequirements = {
  apiKeys: 0,
  files: 0,
  connectedAccounts: 0,
};
const emailNodes: WorkflowNode[] = [
  { id: "start", type: "trigger", template: "webhook", x: 0, y: 0 },
  {
    id: "email",
    type: "action",
    template: "email",
    emailProvider: "sendgrid",
    emailSubject: "Workflow result",
    x: 100,
    y: 0,
  },
];

function preview(
  nodes: WorkflowNode[],
  requirements: ShareImportRequirements | undefined,
  variant: "page" | "dialog" = "page",
) {
  return render(
    <SharedWorkflowCard
      name="Webhook email"
      nodeCount={nodes.length}
      edgeCount={0}
      graph={{ nodes, edges: [] }}
      requirements={requirements}
      variant={variant}
    />,
  );
}

function expectEmailSetup() {
  expect(screen.getByText("Your own email provider API key, for one email action")).toBeTruthy();
  expect(screen.getByText("Your sender and recipient email addresses, for one email action")).toBeTruthy();
  expect(screen.queryByText("Ready to run as-is")).toBeNull();
  expect(screen.queryByText(/Nothing to configure/)).toBeNull();
}

describe("email import readiness", () => {
  const variants: ("page" | "dialog")[] = ["page", "dialog"];
  it.each(variants)("lists both missing inputs in the %s link preview", (variant) => {
    preview(emailNodes, { ...emptyRequirements, emailActions: 1 }, variant);
    expectEmailSetup();
    expect(screen.getByText("You'll need to add")).toBeTruthy();
    expect(screen.queryByText(/for one model node/)).toBeNull();
  });

  it("derives email setup when an older response has no email count", () => {
    preview(emailNodes, emptyRequirements);
    expectEmailSetup();
  });

  it("lists known code setup without claiming all requirements were checked", () => {
    const codeNodes: WorkflowNode[] = [
      ...emailNodes,
      {
        id: "model",
        type: "provider",
        keyMode: "byok",
        apiKey: "private-model-key",
        x: 200,
        y: 0,
      },
    ];
    const { container } = preview(
      codeNodes.map((node) => node.id === "email" ? {
        ...node,
        emailApiKey: "private-email-key",
        emailTo: "recipient@example.invalid",
        emailFrom: "sender@example.invalid",
      } : node),
      undefined,
      "dialog",
    );
    expectEmailSetup();
    expect(screen.getByText("What it may still need")).toBeTruthy();
    expect(screen.getByText(/any model keys or connected accounts/)).toBeTruthy();
    for (const value of ["private-model-key", "private-email-key", "recipient@example.invalid", "sender@example.invalid"]) {
      expect(container.textContent).not.toContain(value);
    }
  });

  it("counts separate email actions once across graph and server requirements", () => {
    preview([
      ...emailNodes,
      { id: "second", type: "action", template: "email", x: 200, y: 0 },
    ], { ...emptyRequirements, emailActions: 2 });
    expect(screen.getByText("Your own email provider API keys, for 2 email actions")).toBeTruthy();
    expect(screen.getByText("Your sender and recipient email addresses, for 2 email actions")).toBeTruthy();
    expect(screen.queryByText("Ready to run as-is")).toBeNull();
  });

  it("preserves the no-setup result for a nonemail platform workflow", () => {
    preview([
      { id: "start", type: "trigger", template: "webhook", x: 0, y: 0 },
      { id: "model", type: "provider", keyMode: "platform", x: 100, y: 0 },
    ], emptyRequirements);
    expect(screen.getByText("Ready to run as-is")).toBeTruthy();
    expect(screen.queryByText(/email provider API key/)).toBeNull();
  });

  it("does not treat an unrelated email template as an email action", () => {
    preview([{ id: "start", type: "trigger", template: "email", x: 0, y: 0 }], emptyRequirements);
    expect(screen.queryByText(/email provider API key/)).toBeNull();
  });

  it("preserves other configuration requirements", () => {
    preview([], { apiKeys: 1, files: 1, connectedAccounts: 0, connectors: ["github"] });
    expect(screen.getByText("Your own API key, for one model node")).toBeTruthy();
    expect(screen.getByText("Your own Github account, reconnected once")).toBeTruthy();
    expect(screen.getByText("A file to upload, where the original had one")).toBeTruthy();
    expect(screen.queryByText("Ready to run as-is")).toBeNull();
    expect(screen.queryByText(/email provider API key/)).toBeNull();
  });
});
