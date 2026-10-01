import type { Workflow, WorkflowNode } from "./types";

// A workflow's agents as its detail screen lists them: the model each thinks
// with (the provider attached to its model port) and how many tools it can
// call (nodes attached to its tools port). Read from the stored graph.
export interface WorkflowAgent {
  id: string;
  name: string;
  model: string | null;
  tools: number;
}

// Nodes that are not steps of the flow: the trigger starts it, and a provider
// only ever serves an agent as its model.
const NOT_STEPS = new Set(["trigger", "provider"]);

function nodeName(n: WorkflowNode): string {
  return n.name || n.label || n.template || n.type;
}

export function workflowAgents(wf: Workflow): WorkflowAgent[] {
  const byId = new Map(wf.nodes.map((n) => [n.id, n]));
  return wf.nodes
    .filter((n) => n.type === "agent")
    .map((agent) => {
      const attached = wf.edges.filter(
        (e) => e.kind === "attach" && e.to === agent.id,
      );
      const provider = attached
        .filter((e) => e.toPort === "model")
        .map((e) => byId.get(e.from))
        .find((n) => n?.type === "provider");
      return {
        id: agent.id,
        name: nodeName(agent),
        model: provider ? provider.model || provider.template || null : null,
        tools: attached.filter((e) => e.toPort === "tools").length,
      };
    });
}

function triggerSentence(wf: Workflow): string {
  if (wf.scheduleCron) return "Runs on a schedule.";
  if (wf.geofenceRadiusM !== undefined)
    return "Runs on arriving at or leaving a place.";
  const trigger = wf.nodes.find((n) => n.type === "trigger");
  switch (trigger?.template) {
    case "chat":
      return "Starts from a chat message.";
    case "webhook":
      return "Runs when its webhook is called.";
    default:
      return "Runs when started.";
  }
}

const plural = (n: number, one: string, many: string) =>
  `${n} ${n === 1 ? one : many}`;

// A plain summary of what a workflow is made of, for when nobody has written
// a description. Built only from the graph, so it never claims more than the
// workflow actually does.
export function describeWorkflow(wf: Workflow): string {
  const agents = workflowAgents(wf);
  // The tools the agents can call -- the same attach-edge counts the Agents
  // list shows, so the sentence and the list cannot disagree. A flow step
  // after an agent (an action, a Google call) is not one of its tools.
  const tools = agents.reduce((n, a) => n + a.tools, 0);
  const steps = wf.nodes.filter((n) => !NOT_STEPS.has(n.type)).length;
  const parts = [triggerSentence(wf)];
  if (agents.length > 0) {
    const models = [
      ...new Set(agents.map((a) => a.model).filter((m): m is string => !!m)),
    ];
    parts.push(
      `${plural(agents.length, "agent", "agents")}${models.length ? ` (${models.join(", ")})` : ""}` +
        (tools ? ` using ${plural(tools, "tool", "tools")}.` : "."),
    );
  } else if (steps) {
    parts.push(`${plural(steps, "step", "steps")}, no agents.`);
  } else {
    parts.push("Nothing has been added to it yet.");
  }
  return parts.join(" ");
}
