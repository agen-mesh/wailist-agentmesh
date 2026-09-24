import { WORKFLOWS } from "./data";
import type { Workflow, WorkflowEdge, WorkflowNode } from "./types";

// The graph behind each workflow in the mock list (NEXT_PUBLIC_API_URL unset).
//
// Every row in WORKFLOWS used to open the same chat-triggered weather sample,
// so six different workflows all read "Weather Agent Test" once tapped. Each
// now has a pipeline that fits its name, and a trigger that matches how its
// fixture runs in runFixtures.ts were started. Node ids here are the ones
// those runs' steps point at.

type Graph = Pick<
  Workflow,
  | "nodes"
  | "edges"
  | "scheduleCron"
  | "geofenceLat"
  | "geofenceLng"
  | "geofenceRadiusM"
  | "geofenceInside"
>;

const flow = (id: string, from: string, to: string): WorkflowEdge => ({
  id,
  from,
  to,
  kind: "flow",
  toPort: "in",
});
const model = (id: string, from: string, to: string): WorkflowEdge => ({
  id,
  from,
  to,
  kind: "attach",
  toPort: "model",
});
const tool = (id: string, from: string, to: string): WorkflowEdge => ({
  id,
  from,
  to,
  kind: "attach",
  toPort: "tools",
});

function paidTool(
  id: string,
  x: number,
  y: number,
  name: string,
  price: string,
  description: string,
): WorkflowNode {
  return {
    id,
    type: "tool402",
    custom: true,
    x,
    y,
    name,
    description,
    endpoint: `https://x402.example.com/${name.toLowerCase().replace(/\s+/g, "-")}`,
    price,
    unit: "call",
    priceLive: true,
  };
}

const GRAPHS: Record<string, Graph> = {
  "wf-triage": {
    nodes: [
      {
        id: "t1",
        type: "trigger",
        template: "webhook",
        x: 80,
        y: 220,
        label: "Helpdesk ticket",
      },
      {
        id: "t2",
        type: "agent",
        template: "router",
        x: 360,
        y: 200,
        name: "Triage Agent",
        systemPrompt:
          "Read the new support ticket. Look up the order it mentions, decide whether it is a delivery, refund, product or account issue, and how urgent it is.",
      },
      {
        id: "t3",
        type: "provider",
        template: "anthropic",
        x: 260,
        y: 440,
        name: "Claude Sonnet",
        model: "claude-sonnet-4-6",
      },
      paidTool(
        "t4",
        420,
        440,
        "Order Lookup",
        "0.012",
        "Order status, items and shipping address for an order number.",
      ),
      paidTool(
        "t5",
        580,
        440,
        "Carrier Tracking",
        "0.018",
        "Live tracking events for a parcel from the major carriers.",
      ),
      paidTool(
        "t6",
        740,
        440,
        "Sentiment Score",
        "0.005",
        "How upset a customer message reads, from 0 (calm) to 1 (furious).",
      ),
      {
        id: "t7",
        type: "agent",
        template: "agent",
        x: 660,
        y: 200,
        name: "Reply Writer",
        systemPrompt:
          "Write a short, specific reply to the customer using what the Triage Agent found. Never promise a refund the policy does not allow.",
      },
      {
        id: "t8",
        type: "tool",
        template: "http",
        x: 940,
        y: 200,
        name: "Post reply to helpdesk",
        url: "https://helpdesk.example.com/api/tickets",
        method: "POST",
      },
      { id: "t9", type: "end", template: "done", x: 1180, y: 210 },
    ],
    edges: [
      flow("te1", "t1", "t2"),
      model("te2", "t3", "t2"),
      tool("te3", "t4", "t2"),
      tool("te4", "t5", "t2"),
      tool("te5", "t6", "t2"),
      flow("te6", "t2", "t7"),
      flow("te7", "t7", "t8"),
      flow("te8", "t8", "t9"),
    ],
  },

  "wf-brief": {
    scheduleCron: "0 7 * * 1-5",
    nodes: [
      {
        id: "b1",
        type: "trigger",
        template: "manual",
        x: 80,
        y: 220,
        label: "Weekdays at 7:00",
      },
      paidTool(
        "b2",
        300,
        440,
        "Market Quotes",
        "0.040",
        "Closing and pre-market prices for indices, rates and commodities.",
      ),
      paidTool(
        "b3",
        460,
        440,
        "News Headlines",
        "0.060",
        "The last 24 hours of market-moving headlines, one page per call.",
      ),
      paidTool(
        "b4",
        620,
        440,
        "FX Rates",
        "0.040",
        "Spot rates for the major currency pairs.",
      ),
      {
        id: "b5",
        type: "agent",
        template: "agent",
        x: 440,
        y: 200,
        name: "Brief Writer",
        systemPrompt:
          "Write a pre-market brief for the desk: what moved overnight, why, and what to watch today. Five bullets at most, numbers first.",
      },
      {
        id: "b6",
        type: "provider",
        template: "openai",
        x: 780,
        y: 440,
        name: "GPT-4.1",
        model: "gpt-4.1",
      },
      {
        id: "b7",
        type: "action",
        template: "email",
        x: 760,
        y: 200,
        name: "Email the desk",
        emailTo: "desk@acme-capital.example",
        emailSubject: "Pre-market brief",
      },
      { id: "b8", type: "end", template: "done", x: 1000, y: 210 },
    ],
    edges: [
      flow("be1", "b1", "b5"),
      tool("be2", "b2", "b5"),
      tool("be3", "b3", "b5"),
      tool("be4", "b4", "b5"),
      model("be5", "b6", "b5"),
      flow("be6", "b5", "b7"),
      flow("be7", "b7", "b8"),
    ],
  },

  "wf-invoice": {
    nodes: [
      {
        id: "i1",
        type: "trigger",
        template: "manual",
        x: 80,
        y: 220,
        label: "Month-end close",
      },
      paidTool(
        "i2",
        300,
        220,
        "Invoice OCR",
        "0.003",
        "Reads supplier invoices (PDF or photo) into line items. Priced per page.",
      ),
      {
        id: "i3",
        type: "tool",
        template: "http",
        x: 540,
        y: 220,
        name: "Fetch bank feed",
        url: "https://bank.example.com/api/transactions",
        method: "GET",
      },
      {
        id: "i4",
        type: "agent",
        template: "agent",
        x: 780,
        y: 200,
        name: "Reconciliation Agent",
        systemPrompt:
          "Match each invoice to a bank payment by amount, date and supplier. List anything unmatched or paid twice.",
      },
      {
        id: "i5",
        type: "provider",
        template: "gemini",
        x: 780,
        y: 440,
        name: "Gemini 2.5 Flash",
        model: "gemini-2.5-flash",
      },
      {
        id: "i6",
        type: "action",
        template: "email",
        x: 1020,
        y: 200,
        name: "Email finance",
        emailTo: "finance@acme-capital.example",
        emailSubject: "Reconciliation exceptions",
      },
      { id: "i7", type: "end", template: "done", x: 1240, y: 210 },
    ],
    edges: [
      flow("ie1", "i1", "i2"),
      flow("ie2", "i2", "i3"),
      flow("ie3", "i3", "i4"),
      model("ie4", "i5", "i4"),
      flow("ie5", "i4", "i6"),
      flow("ie6", "i6", "i7"),
    ],
  },

  "wf-leads": {
    nodes: [
      {
        id: "l1",
        type: "trigger",
        template: "manual",
        x: 80,
        y: 220,
        label: "New lead",
      },
      paidTool(
        "l2",
        300,
        440,
        "Company Enrichment",
        "0.025",
        "Headcount, funding, industry and tech stack for a company domain.",
      ),
      {
        id: "l3",
        type: "agent",
        template: "agent",
        x: 380,
        y: 200,
        name: "Lead Scorer",
        systemPrompt:
          "Score the lead from 0 to 100 against our ideal customer profile and give the two strongest reasons.",
      },
      {
        id: "l4",
        type: "provider",
        template: "anthropic",
        x: 480,
        y: 440,
        name: "Claude Sonnet",
        model: "claude-sonnet-4-6",
      },
      {
        id: "l5",
        type: "action",
        template: "hubspot",
        x: 660,
        y: 200,
        name: "Update CRM",
      },
      { id: "l6", type: "end", template: "done", x: 900, y: 210 },
    ],
    edges: [
      flow("le1", "l1", "l3"),
      tool("le2", "l2", "l3"),
      model("le3", "l4", "l3"),
      flow("le4", "l3", "l5"),
      flow("le5", "l5", "l6"),
    ],
  },

  "wf-onchain": {
    geofenceLat: 37.7897,
    geofenceLng: -122.3972,
    geofenceRadiusM: 250,
    geofenceInside: false,
    nodes: [
      {
        id: "o1",
        type: "trigger",
        template: "manual",
        x: 80,
        y: 220,
        label: "Arrive at the office",
      },
      paidTool(
        "o2",
        300,
        440,
        "Wallet Screening",
        "0.008",
        "Risk exposure for a wallet: mixers, hacks, darknet markets.",
      ),
      paidTool(
        "o3",
        460,
        440,
        "Sanctions List",
        "0.001",
        "Checks an address against OFAC, EU and UN sanctions lists.",
      ),
      {
        id: "o4",
        type: "agent",
        template: "agent",
        x: 380,
        y: 200,
        name: "Risk Reviewer",
        systemPrompt:
          "Review yesterday's inbound transfers to the treasury wallet. Screen each counterparty and summarise anything a compliance officer must look at.",
      },
      {
        id: "o5",
        type: "provider",
        template: "gemini",
        x: 620,
        y: 440,
        name: "Gemini 2.5 Flash",
        model: "gemini-2.5-flash",
      },
      {
        id: "o6",
        type: "action",
        template: "slack",
        x: 660,
        y: 200,
        name: "Alert #compliance",
      },
      { id: "o7", type: "end", template: "done", x: 900, y: 210 },
    ],
    edges: [
      flow("oe1", "o1", "o4"),
      tool("oe2", "o2", "o4"),
      tool("oe3", "o3", "o4"),
      model("oe4", "o5", "o4"),
      flow("oe5", "o4", "o6"),
      flow("oe6", "o6", "o7"),
    ],
  },

  "wf-content": {
    scheduleCron: "0 9 * * 1",
    nodes: [
      {
        id: "c1",
        type: "trigger",
        template: "manual",
        x: 80,
        y: 220,
        label: "Mondays at 9:00",
      },
      {
        id: "c2",
        type: "agent",
        template: "agent",
        x: 320,
        y: 200,
        name: "Topic Researcher",
        systemPrompt:
          "Find three topics our customers asked about this week that we have not written about yet.",
      },
      {
        id: "c3",
        type: "agent",
        template: "agent",
        x: 580,
        y: 200,
        name: "Post Drafter",
        systemPrompt:
          "Draft a 600-word blog post on the strongest topic, in our house style.",
      },
      {
        id: "c4",
        type: "provider",
        template: "openai",
        x: 450,
        y: 440,
        name: "GPT-4.1",
        model: "gpt-4.1",
      },
      {
        id: "c5",
        type: "action",
        template: "notion",
        x: 840,
        y: 200,
        name: "Add to review queue",
      },
      { id: "c6", type: "end", template: "done", x: 1080, y: 210 },
    ],
    edges: [
      flow("ce1", "c1", "c2"),
      model("ce2", "c4", "c2"),
      flow("ce3", "c2", "c3"),
      model("ce4", "c4", "c3"),
      flow("ce5", "c3", "c5"),
      flow("ce6", "c5", "c6"),
    ],
  },
};

// The full workflow for a mock list id, or null for an id the list does not
// have. A fresh copy each time, so an edit on the canvas cannot leak into the
// next screen that opens it.
export function fixtureWorkflow(id: string): Workflow | null {
  const row = WORKFLOWS.find((w) => w.id === id);
  const graph = GRAPHS[id];
  if (!row || !graph) return null;
  return JSON.parse(JSON.stringify({ ...row, ...graph })) as Workflow;
}

// A node of a mock workflow, for the run steps that refer to it by id.
export function fixtureNode(
  workflowId: string,
  nodeId: string,
): WorkflowNode | undefined {
  return GRAPHS[workflowId]?.nodes.find((n) => n.id === nodeId);
}
