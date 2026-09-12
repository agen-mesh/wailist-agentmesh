package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/bazaar"
	"github.com/agentmesh/backend/internal/models"
)

// The builder's view of the x402 Bazaar: search the catalog, then add a
// chosen entry as a tool402 node. The model never types an endpoint URL or a
// price -- it picks an id from search results and the server fills the node
// in from the catalog entry. A mistyped URL here is not a 404, it is a
// payment to the wrong place.

// x402SearchLimit keeps a result set the model can actually compare.
const x402SearchLimit = 8

// x402Session loads the catalog at most once per build, and only if the
// model actually asks for it: most builds never touch x402, and a cold
// catalog is a multi-page crawl of the upstream registry.
type x402Session struct {
	load   func(ctx context.Context) ([]bazaar.Resource, error)
	items  []bazaar.Resource
	loaded bool
}

func newX402Session(load func(ctx context.Context) ([]bazaar.Resource, error)) *x402Session {
	return &x402Session{load: load}
}

func (x *x402Session) catalog(ctx context.Context) ([]bazaar.Resource, error) {
	if x.load == nil {
		return nil, fmt.Errorf("the x402 catalog is not available right now -- build this with an http tool node, a connector or websearch instead")
	}
	if !x.loaded {
		items, err := x.load(ctx)
		if err != nil {
			// Not wrapped: the underlying error names upstream hosts and
			// transport detail, and this text goes back to the model and on
			// into the user's chat.
			return nil, fmt.Errorf("the x402 catalog could not be loaded right now -- try again later, or build this with an http tool node, a connector or websearch instead")
		}
		x.items, x.loaded = items, true
	}
	return x.items, nil
}

// formatMicros renders atomic 6-decimal units the way the frontend's
// formatPrice does: 5000 -> "0.005", no trailing zeros.
func formatMicros(m int64) string {
	return strconv.FormatFloat(float64(m)/1e6, 'f', -1, 64)
}

// x402CallCost states what one call really costs the user: the endpoint's
// own price AND the AgentMesh fee the relay adds to every paid call. The fee
// is usually the larger part by far, so quoting the endpoint price alone
// would understate the cost several times over.
func x402CallCost(r bazaar.Resource) string {
	return fmt.Sprintf("%s %s to the endpoint + %s USD AgentMesh fee, per call",
		formatMicros(r.AmountMicros), assetSymbol(r.Asset), formatMicros(models.X402PlatformFeeUSDMicros))
}

func x402DisplayName(r bazaar.Resource) string {
	if r.Supported && r.Provider != "" {
		return r.Provider
	}
	return r.Host
}

func (x *x402Session) search(ctx context.Context, args map[string]any) (string, error) {
	query := strings.TrimSpace(argString(args, "query"))
	if query == "" {
		return "", fmt.Errorf("search_x402: query is required")
	}
	items, err := x.catalog(ctx)
	if err != nil {
		return "", err
	}
	type result struct {
		ID            string         `json:"id"`
		Name          string         `json:"name"`
		Method        string         `json:"method"`
		URL           string         `json:"url"`
		Description   string         `json:"description"`
		Cost          string         `json:"cost"`
		Network       string         `json:"network"`
		Params        []bazaar.Param `json:"params"`
		OutputExample string         `json:"outputExample,omitempty"`
		TimesPaid     int            `json:"timesPaid"`
	}
	hits := bazaar.Search(items, query, x402SearchLimit)
	results := make([]result, 0, len(hits))
	for _, r := range hits {
		network := "algorand mainnet"
		if r.Testnet {
			network = "algorand testnet"
		}
		example := r.OutputExample
		if len(example) > 300 {
			example = example[:300] + "…"
		}
		results = append(results, result{
			ID: r.ID, Name: x402DisplayName(r), Method: r.Method, URL: r.URL,
			Description: r.Description, Cost: x402CallCost(r), Network: network,
			Params: r.Params, OutputExample: example, TimesPaid: r.SettleCount,
		})
	}
	out := map[string]any{"results": results}
	if len(results) == 0 {
		out["note"] = "No x402 endpoint matched. Try different keywords, or build this with an http tool node, a connector or websearch."
	} else {
		out["note"] = "Every call costs the user the endpoint price plus the AgentMesh fee shown. timesPaid is how often real callers have paid it -- prefer proven endpoints. Add one with add_x402_node and the id."
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (x *x402Session) add(ctx context.Context, graph *models.WorkflowGraph, args map[string]any) (string, error) {
	id := strings.TrimSpace(argString(args, "id"))
	items, err := x.catalog(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range items {
		if r.ID != id || r.Console != "" {
			continue
		}
		node := x402NodeFromResource(r, newGraphID("n_"),
			80+240*float64(len(graph.Nodes)%4), 120+160*float64(len(graph.Nodes)/4),
			argString(args, "name"))
		graph.Nodes = append(graph.Nodes, node)
		params := make([]string, len(r.Params))
		for i, p := range r.Params {
			params[i] = p.Name
		}
		msg := fmt.Sprintf("added x402 node %s: %s %s -- costs %s. Tell the user that cost in your reply.",
			node.ID, r.Method, r.URL, x402CallCost(r))
		if len(params) > 0 {
			msg += " Its inputs (" + strings.Join(params, ", ") + ") are filled per call by the agent it is attached to, so attach it to an agent's tools port."
		}
		return msg, nil
	}
	return "", fmt.Errorf("add_x402_node: no x402 endpoint with id %q in the catalog -- call search_x402 and use an id it returned", id)
}

// x402NodeFromResource builds the tool402 node for a catalog entry. It is
// the Go twin of the frontend's resourceToNode (frontend/src/lib/bazaar.ts),
// which is what adding a Bazaar card to a canvas does -- keep the two in
// step, so an endpoint the builder adds is the same node a person would get.
//
// The address goes in Endpoint: tool402 calls node.Endpoint and never reads
// node.URL.
func x402NodeFromResource(r bazaar.Resource, id string, x, y float64, name string) models.WorkflowNode {
	params := make([]models.ParamDef, len(r.Params))
	// Catalog param examples are placeholders, never usable values -- seed
	// every default empty, exactly as resourceToNode does.
	defaults := make(map[string]string, len(r.Params))
	for i, p := range r.Params {
		params[i] = models.ParamDef{Name: p.Name, Type: p.Type, Required: p.Required, Description: p.Description}
		defaults[p.Name] = ""
	}
	provider := x402DisplayName(r)
	if name == "" {
		name = provider
	}
	return models.WorkflowNode{
		ID:               id,
		Type:             models.NodeTypeTool402,
		Name:             name,
		Icon:             "✦",
		X:                x,
		Y:                y,
		Endpoint:         r.URL,
		Method:           r.Method,
		Description:      r.Description,
		Price:            formatMicros(r.AmountMicros),
		Unit:             "call",
		Provider:         provider,
		DiscoveredParams: params,
		ParamDefaults:    defaults,
	}
}
