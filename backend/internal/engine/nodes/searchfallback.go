package nodes

import "github.com/agentmesh/backend/internal/models"

// readsLiveData reports whether n fetches from outside the workflow -- the
// kind of step that can go down, and so the kind a search fallback exists
// for.
//
// Narrower than IsDegradable on purpose. calc, json_extract, set and the
// other pure tools are reads too (their failure may degrade), but they read
// nothing live: an agent whose workflow only does arithmetic has no source
// that can be unavailable, and giving it a search tool would only invite it
// to search for things it was never meant to look up. state/get reads this
// workflow's own stored values, which are not a live source either.
func readsLiveData(n models.WorkflowNode) bool {
	switch n.Type {
	case models.NodeTypeTool:
		return n.Template == "http" && IsDegradable(n)
	case models.NodeTypeAction, models.NodeTypeGoogle:
		return IsDegradable(n)
	}
	return false
}

// liveDataIDs is the set of nodes in graph that read live data, by id. Taken
// before a build runs and compared after, it answers the only question
// ensureSearchFallback needs: did THIS build introduce a live source?
func liveDataIDs(graph models.WorkflowGraph) map[string]bool {
	ids := map[string]bool{}
	for _, n := range graph.Nodes {
		if readsLiveData(n) {
			ids[n.ID] = true
		}
	}
	return ids
}

// ensureSearchFallback attaches a websearch tool to the agent of a workflow
// whose live-data sources this build added.
//
// This is what makes degradation useful rather than merely honest: when a
// read step fails, degradedInputGuard tells the agent to look the value up,
// and it can only do that if the tool is there. Deterministic rather than a
// prompt rule, for the same reason a provider's keyMode default is -- this
// has to hold every time, not most of the time.
//
// known is the set of live-data node ids the build started with. Only a
// source outside that set triggers the attach, which is what keeps the tool
// from being forced on the user: deleting the Web Search node from a
// finished workflow used to bring it straight back on the next message,
// since a plain "is one attached right now?" check cannot tell a node the
// user removed from one that was never offered. It also means a turn that
// asks a question, or edits a prompt, no longer silently adds a node (and a
// billable tool) to a canvas the user did not ask to change.
//
// The tool goes to the last agent in the node list, which on a builder-made
// graph is the most recently added one and, in practice, the one composing
// the reply. The edge is added through addGraphEdge rather than built by
// hand so that it passes exactly the validation a model-requested attach
// would. Reports whether it changed anything; it is idempotent for a given
// known set, so calling it on every reply attempt of a build is safe.
func ensureSearchFallback(graph *models.WorkflowGraph, known map[string]bool) bool {
	var agentID string
	live := false
	for _, n := range graph.Nodes {
		if n.Type == models.NodeTypeAgent {
			agentID = n.ID
		}
		if readsLiveData(n) && !known[n.ID] {
			live = true
		}
	}
	if agentID == "" || !live {
		return false
	}
	// Checked by edge rather than by node: an unattached websearch node
	// elsewhere in the graph does not give this agent the ability to search.
	attached := map[string]bool{}
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindAttach && e.To == agentID {
			attached[e.From] = true
		}
	}
	for _, n := range graph.Nodes {
		if attached[n.ID] && n.Type == models.NodeTypeTool && n.Template == "websearch" {
			return false
		}
	}

	// Built through addGraphNode, not by hand, for the same reason the edge
	// below goes through addGraphEdge: one code path, so this node gets
	// whatever every model-added node gets. Hand-building it meant copying
	// the grid placement formula and missing the read-retry default -- and a
	// default added there later would silently skip this node.
	if _, err := addGraphNode(graph, map[string]any{
		"type": "tool", "template": "websearch", "name": "Web Search",
	}); err != nil {
		return false
	}
	// addGraphNode appends, so the node it just made is the last one. It
	// returns a sentence for the model rather than an id, and parsing the id
	// back out of that sentence would break the moment the wording changes.
	id := graph.Nodes[len(graph.Nodes)-1].ID
	if _, err := addGraphEdge(graph, map[string]any{
		"from":   id,
		"to":     agentID,
		"kind":   string(models.EdgeKindAttach),
		"toPort": "tools",
	}); err != nil {
		// A search node wired to nothing would be an audit finding of its
		// own, so take it back out rather than leave it on the canvas.
		graph.Nodes = graph.Nodes[:len(graph.Nodes)-1]
		return false
	}
	return true
}
