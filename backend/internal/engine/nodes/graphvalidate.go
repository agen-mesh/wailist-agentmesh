package nodes

import (
	"fmt"
	"sort"

	"github.com/agentmesh/backend/internal/models"
)

// Edge legality for chat-built graphs, mirroring the canvas's own
// isValidConnection (frontend/src/lib/portUtils.ts). Until this existed,
// addGraphEdge accepted any edge between two real node ids, and the two ways
// that goes wrong are both silent:
//
//   - A backwards attach (agent -> provider) is dropped by BuildAttachMap
//     (engine/graph.go) with nothing but a server log. The canvas draws the
//     line, the model reports success, and the run has no model attached.
//   - An attach edge with an empty toPort matches neither "model" nor
//     "tools" in BuildAttachMap's switch, so it is inert -- and the canvas
//     renders it from the wrong port too, because `e.toPort ?? portForTo(b)`
//     does not fall back on "".
//
// Rejecting here turns both into an error string the build loop feeds back
// to the model as a functionResponse, which it can act on, instead of a
// success it cannot.

// attachSources maps a node type to the agent port it may attach to. A type
// absent from this map may not be an attach source at all.
var attachSources = map[models.NodeType]string{
	models.NodeTypeProvider: "model",
	models.NodeTypeTool:     "tools",
	models.NodeTypeTool402:  "tools",
}

// flowSources / flowTargets mirror isValidConnection's flow branch. Provider
// is deliberately absent from both: a standalone provider flow step is a
// backend no-op, so it stays attach-only. Trigger is absent from flowTargets
// because nothing flows into a trigger.
var flowSources = map[models.NodeType]bool{
	models.NodeTypeTrigger: true, models.NodeTypeAgent: true,
	models.NodeTypeAction: true, models.NodeTypeState: true,
	models.NodeTypeTool: true, models.NodeTypeTool402: true,
	models.NodeTypeTendril: true, models.NodeTypeGoogle: true,
}

var flowTargets = map[models.NodeType]bool{
	models.NodeTypeAgent: true, models.NodeTypeAction: true,
	models.NodeTypeState: true, models.NodeTypeEnd: true,
	models.NodeTypeTool: true, models.NodeTypeTool402: true,
	models.NodeTypeTendril: true, models.NodeTypeGoogle: true,
}

// validateEdge reports whether an edge is legal and returns the toPort to
// store. The returned port is normalised, never empty: an omitted port is
// filled in from the source type for an attach and is always "in" for a
// flow, so a stored edge can never be the inert empty-port kind.
func validateEdge(graph *models.WorkflowGraph, from, to, kind, toPort string) (string, error) {
	src, ok := findGraphNode(graph, from)
	if !ok {
		return "", fmt.Errorf("add_edge: node %q not found", from)
	}
	dst, ok := findGraphNode(graph, to)
	if !ok {
		return "", fmt.Errorf("add_edge: node %q not found", to)
	}

	if kind == string(models.EdgeKindAttach) {
		if dst.Type != models.NodeTypeAgent {
			return "", fmt.Errorf(
				"add_edge: an attach edge must end at an agent node, but %q is a %s -- attach edges go provider/tool -> agent",
				to, dst.Type)
		}
		want, ok := attachSources[src.Type]
		if !ok {
			return "", fmt.Errorf(
				"add_edge: a %s node cannot attach to an agent (only provider, tool and tool402 can) -- if you meant the execution path, use kind=\"flow\"; note the direction is provider -> agent, never agent -> provider",
				src.Type)
		}
		if toPort == "" {
			return want, nil
		}
		if toPort != want {
			return "", fmt.Errorf(
				"add_edge: a %s node attaches to the %q port, not %q",
				src.Type, want, toPort)
		}
		return want, nil
	}

	if !flowSources[src.Type] {
		if src.Type == models.NodeTypeProvider {
			return "", fmt.Errorf(
				"add_edge: a provider node is never a flow step -- attach it to an agent's \"model\" port with kind=\"attach\" instead")
		}
		return "", fmt.Errorf("add_edge: a %s node cannot start a flow edge", src.Type)
	}
	if !flowTargets[dst.Type] {
		if dst.Type == models.NodeTypeTrigger {
			return "", fmt.Errorf("add_edge: nothing flows into a trigger -- a trigger is where the workflow starts")
		}
		return "", fmt.Errorf("add_edge: a flow edge cannot end at a %s node", dst.Type)
	}
	if from == to {
		return "", fmt.Errorf("add_edge: a node cannot flow into itself")
	}
	// Longer loops too: the engine topologically sorts the flow and fails
	// every run of a graph that loops ("cycle detected in workflow graph").
	if flowReaches(*graph, to, from) {
		return "", fmt.Errorf("add_edge: connecting %q -> %q would create a loop, because %q already leads back to %q -- a workflow's flow cannot loop", from, to, to, from)
	}
	return "in", nil
}

// flowReaches reports whether dst can be reached from src along flow edges.
func flowReaches(graph models.WorkflowGraph, src, dst string) bool {
	next := map[string][]string{}
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindFlow {
			next[e.From] = append(next[e.From], e.To)
		}
	}
	seen := map[string]bool{}
	queue := []string{src}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == dst {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		queue = append(queue, next[id]...)
	}
	return false
}

// flowLoopNode returns a node that sits on a loop of flow edges, or "" when
// the flow has none. A loop drawn by hand never passes through validateEdge.
func flowLoopNode(graph models.WorkflowGraph) string {
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindFlow && flowReaches(graph, e.To, e.From) {
			return e.From
		}
	}
	return ""
}

func findGraphNode(graph *models.WorkflowGraph, id string) (models.WorkflowNode, bool) {
	for _, n := range graph.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return models.WorkflowNode{}, false
}

// auditGraph lists what is still wrong with the graph as a whole, in the
// user's terms. Per-edge validation cannot catch these: an agent with no
// model, a graph with no trigger and a node wired to nothing are all made of
// individually legal edges (or of no edges at all).
//
// Findings are sorted so the same broken graph always produces the same
// message -- the text goes back into the model's context, and an unstable
// ordering there makes a build non-reproducible for no reason.
func auditGraph(graph models.WorkflowGraph) []string {
	var findings []string

	if len(graph.Nodes) == 0 {
		return nil
	}

	hasTrigger := false
	agents := make([]string, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		if n.Type == models.NodeTypeTrigger {
			hasTrigger = true
		}
		if n.Type == models.NodeTypeAgent {
			agents = append(agents, n.ID)
		}
	}
	if !hasTrigger {
		findings = append(findings, "the workflow has no trigger node, so nothing can start it -- add one (template \"chat\" for a conversational workflow, \"manual\" for a button-started one) and flow it into the agent")
	}

	byID := make(map[string]models.WorkflowNode, len(graph.Nodes))
	for _, n := range graph.Nodes {
		byID[n.ID] = n
	}

	hasModel := map[string]bool{}
	connected := map[string]bool{}
	for _, e := range graph.Edges {
		connected[e.From] = true
		connected[e.To] = true
		if e.Kind == models.EdgeKindAttach && e.ToPort == "model" &&
			byID[e.From].Type == models.NodeTypeProvider {
			hasModel[e.To] = true
		}
	}

	for _, id := range agents {
		if !hasModel[id] {
			findings = append(findings, fmt.Sprintf(
				"agent %q has no model: add a provider node and connect it with add_edge(from=<provider id>, to=%q, kind=\"attach\", toPort=\"model\")",
				id, id))
		}
	}

	for _, n := range graph.Nodes {
		if !connected[n.ID] {
			findings = append(findings, fmt.Sprintf(
				"node %q (%s/%s) is not connected to anything -- either wire it up or remove it",
				n.ID, n.Type, n.Template))
		}
	}

	// A node can be connected to something and still never be reached: the
	// failed Nifty/Sensex run wired "Extract Sensex Price" onward into
	// "Combine Prices" but never wired "Fetch Sensex" into it. With nothing
	// flowing in, the engine does not skip such a step -- it runs it FIRST,
	// beside the trigger, on an empty input, and a parser there fails in
	// milliseconds and dead-letters the whole run before any fetch happens.
	// So every flow step must be reachable from a trigger.
	//
	// A flow step is anything the engine runs in the flow: every agent,
	// action, state, end, google and tendril node, and any tool with a flow
	// edge. A provider, or a tool attached only to an agent's tools port, is
	// not one -- the agent calls it. Orphans are already reported above.
	//
	// Same rule as isGraphRunnable (frontend/src/components/canvas/
	// buildModeRelease.ts), which gates the switch back to run mode. If this
	// called such a graph clean, the builder would declare it finished while
	// the canvas refused to leave build mode.
	if loop := flowLoopNode(graph); loop != "" {
		findings = append(findings, fmt.Sprintf(
			"the flow loops back on itself through node %q -- the engine cannot run a loop; remove the edge that closes it",
			loop))
	}

	if hasTrigger {
		reached := reachableFromTriggers(graph)
		onFlowEdge := map[string]bool{}
		for _, e := range graph.Edges {
			if e.Kind == models.EdgeKindFlow {
				onFlowEdge[e.From] = true
				onFlowEdge[e.To] = true
			}
		}
		for _, n := range graph.Nodes {
			if n.Type == models.NodeTypeTrigger || !connected[n.ID] || reached[n.ID] {
				continue
			}
			if !alwaysFlowStep[n.Type] && !onFlowEdge[n.ID] {
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"node %q (%s/%s) is not reached from the trigger -- nothing flows into it, so the engine would run it first with no input; add a flow edge into it from the step whose output it needs",
				n.ID, n.Type, n.Template))
		}
	}

	sort.Strings(findings)
	return findings
}

// alwaysFlowStep are node types that only ever run as steps in the flow.
// Tools are flow steps only when a flow edge touches them; attached to an
// agent's tools port, the agent calls them instead.
var alwaysFlowStep = map[models.NodeType]bool{
	models.NodeTypeAgent: true, models.NodeTypeAction: true, models.NodeTypeState: true,
	models.NodeTypeEnd: true, models.NodeTypeGoogle: true, models.NodeTypeTendril: true,
}

// reachableFromTriggers walks forward from every trigger along flow edges
// only. Attach edges are not part of the execution path -- counting them
// would let a provider shared by two agents look like a route between them.
func reachableFromTriggers(graph models.WorkflowGraph) map[string]bool {
	next := map[string][]string{}
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindFlow {
			next[e.From] = append(next[e.From], e.To)
		}
	}
	seen := map[string]bool{}
	var queue []string
	for _, n := range graph.Nodes {
		if n.Type == models.NodeTypeTrigger {
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		queue = append(queue, next[id]...)
	}
	return seen
}
