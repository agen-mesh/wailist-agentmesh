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

// ensureSearchFallback attaches a websearch tool to the agent of a workflow
// that reads live data and cannot already search.
//
// This is what makes degradation useful rather than merely honest: when a
// read step fails, degradedInputGuard tells the agent to look the value up,
// and it can only do that if the tool is there. Deterministic rather than a
// prompt rule, for the same reason a provider's keyMode default is -- this
// has to hold every time, not most of the time.
//
// The tool goes to the last agent in the node list, which on a builder-made
// graph is the most recently added one and, in practice, the one composing
// the reply. The edge is added through addGraphEdge rather than built by
// hand so that it passes exactly the validation a model-requested attach
// would. Reports whether it changed anything; it is idempotent, so calling
// it on every reply attempt of a build is safe.
func ensureSearchFallback(graph *models.WorkflowGraph) bool {
	var agentID string
	live := false
	for _, n := range graph.Nodes {
		if n.Type == models.NodeTypeAgent {
			agentID = n.ID
		}
		if readsLiveData(n) {
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

	id := newGraphID("n_")
	graph.Nodes = append(graph.Nodes, models.WorkflowNode{
		ID:       id,
		Type:     models.NodeTypeTool,
		Template: "websearch",
		Name:     "Web Search",
		// The same placement addGraphNode uses, so it lands on the grid
		// beside the nodes the model added rather than at the origin.
		X: 80 + 240*float64(len(graph.Nodes)%4),
		Y: 120 + 160*float64(len(graph.Nodes)/4),
	})
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
