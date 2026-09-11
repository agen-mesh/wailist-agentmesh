package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

var graphNodeTypes = map[string]bool{
	"trigger": true, "agent": true, "provider": true, "tool": true,
	"tool402": true, "action": true, "end": true, "tendril": true,
}

var graphEdgeKinds = map[string]bool{"flow": true, "attach": true}

// nodeFieldSetters maps the config keys a build-mode tool call may set onto
// the corresponding WorkflowNode string field. Deliberately excludes
// DiscoveredParams/CustomParams/Secrets/Config/tendril* fields -- those are
// advanced per-node authoring surfaces out of scope for a first cut of
// chat-built graphs.
var nodeFieldSetters = map[string]func(n *models.WorkflowNode, v string){
	"systemPrompt":  func(n *models.WorkflowNode, v string) { n.SystemPrompt = v },
	"model":         func(n *models.WorkflowNode, v string) { n.Model = v },
	"keyMode":       func(n *models.WorkflowNode, v string) { n.KeyMode = v },
	"apiKey":        func(n *models.WorkflowNode, v string) { n.APIKey = v },
	"url":           func(n *models.WorkflowNode, v string) { n.URL = v },
	"method":        func(n *models.WorkflowNode, v string) { n.Method = v },
	"endpoint":      func(n *models.WorkflowNode, v string) { n.Endpoint = v },
	"price":         func(n *models.WorkflowNode, v string) { n.Price = v },
	"unit":          func(n *models.WorkflowNode, v string) { n.Unit = v },
	"provider":      func(n *models.WorkflowNode, v string) { n.Provider = v },
	"description":   func(n *models.WorkflowNode, v string) { n.Description = v },
	"emailTo":       func(n *models.WorkflowNode, v string) { n.EmailTo = v },
	"emailFrom":     func(n *models.WorkflowNode, v string) { n.EmailFrom = v },
	"emailSubject":  func(n *models.WorkflowNode, v string) { n.EmailSubject = v },
	"emailBody":     func(n *models.WorkflowNode, v string) { n.EmailBody = v },
	"emailProvider": func(n *models.WorkflowNode, v string) { n.EmailProvider = v },
}

func newGraphID(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

// applyGraphOp mutates graph in place per a single tool call the build-mode
// meta-agent requested, and returns a short human-readable result string fed
// back to the model as the tool's functionResponse.
func applyGraphOp(graph *models.WorkflowGraph, funcName string, args map[string]any) (string, error) {
	switch funcName {
	case "add_node":
		return addGraphNode(graph, args)
	case "update_node":
		return updateGraphNode(graph, args)
	case "remove_node":
		return removeGraphNode(graph, args)
	case "add_edge":
		return addGraphEdge(graph, args)
	case "remove_edge":
		return removeGraphEdge(graph, args)
	default:
		return "", fmt.Errorf("unknown graph tool %q", funcName)
	}
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func argFields(args map[string]any) (map[string]string, error) {
	out := map[string]string{}
	raw, ok := args["fields"].(map[string]any)
	if !ok {
		return out, nil
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			return nil, fmt.Errorf("field %q must be a string, got %T", k, v)
		}
	}
	return out, nil
}

func addGraphNode(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	nodeType := argString(args, "type")
	if !graphNodeTypes[nodeType] {
		return "", fmt.Errorf("add_node: invalid type %q", nodeType)
	}
	fields, err := argFields(args)
	if err != nil {
		return "", err
	}
	id := newGraphID("n_")
	node := models.WorkflowNode{
		ID:       id,
		Type:     models.NodeType(nodeType),
		Template: argString(args, "template"),
		Name:     argString(args, "name"),
		X:        80 + 240*float64(len(graph.Nodes)%4),
		Y:        120 + 160*float64(len(graph.Nodes)/4),
	}
	for k, v := range fields {
		if set, ok := nodeFieldSetters[k]; ok {
			set(&node, v)
		}
	}
	// Default a new Provider node to platform-key mode unless the model
	// explicitly chose "byok" -- resolveAPIKey (provider.go) treats any
	// KeyMode other than "platform" as BYOK and reads node.APIKey, which a
	// chat-built node never has. Left to the model's own judgment (via the
	// fieldsSchema description alone) this defaulted to "" == BYOK in
	// practice, so every chat-built agent needed a manual Inspector trip
	// before it could run at all. Deterministic here rather than relying on
	// prompt compliance -- this must hold every time, not most of the time.
	if node.Type == models.NodeTypeProvider && node.KeyMode == "" {
		node.KeyMode = "platform"
	}
	graph.Nodes = append(graph.Nodes, node)
	return fmt.Sprintf("added node %s (%s/%s)", id, nodeType, node.Template), nil
}

func updateGraphNode(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	id := argString(args, "id")
	for i := range graph.Nodes {
		if graph.Nodes[i].ID != id {
			continue
		}
		if name := argString(args, "name"); name != "" {
			graph.Nodes[i].Name = name
		}
		if template := argString(args, "template"); template != "" {
			graph.Nodes[i].Template = template
		}
		fields, err := argFields(args)
		if err != nil {
			return "", err
		}
		for k, v := range fields {
			if set, ok := nodeFieldSetters[k]; ok {
				set(&graph.Nodes[i], v)
			}
		}
		return fmt.Sprintf("updated node %s", id), nil
	}
	return "", fmt.Errorf("update_node: node %q not found", id)
}

func removeGraphNode(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	id := argString(args, "id")
	found := false
	nodes := graph.Nodes[:0]
	for _, n := range graph.Nodes {
		if n.ID == id {
			found = true
			continue
		}
		nodes = append(nodes, n)
	}
	if !found {
		return "", fmt.Errorf("remove_node: node %q not found", id)
	}
	graph.Nodes = nodes
	edges := graph.Edges[:0]
	for _, e := range graph.Edges {
		if e.From == id || e.To == id {
			continue
		}
		edges = append(edges, e)
	}
	graph.Edges = edges
	return fmt.Sprintf("removed node %s", id), nil
}

func addGraphEdge(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	from := argString(args, "from")
	to := argString(args, "to")
	kind := argString(args, "kind")
	if kind == "" {
		kind = "flow"
	}
	if !graphEdgeKinds[kind] {
		return "", fmt.Errorf("add_edge: invalid kind %q", kind)
	}
	// Legality and port normalisation together -- see graphvalidate.go for
	// why an unvalidated edge here surfaces as a line on the canvas that the
	// engine silently ignores at run time.
	port, err := validateEdge(graph, from, to, kind, argString(args, "toPort"))
	if err != nil {
		return "", err
	}
	edge := models.WorkflowEdge{
		ID:     newGraphID("e_"),
		From:   from,
		To:     to,
		Kind:   models.EdgeKind(kind),
		ToPort: port,
	}
	graph.Edges = append(graph.Edges, edge)
	return fmt.Sprintf("added edge %s (%s -%s-> %s, port %s)", edge.ID, from, kind, to, port), nil
}

func removeGraphEdge(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	id := argString(args, "id")
	found := false
	edges := graph.Edges[:0]
	for _, e := range graph.Edges {
		if e.ID == id {
			found = true
			continue
		}
		edges = append(edges, e)
	}
	if !found {
		return "", fmt.Errorf("remove_edge: edge %q not found", id)
	}
	graph.Edges = edges
	return fmt.Sprintf("removed edge %s", id), nil
}

func graphToolDecls() []funcDecl {
	// The legal keys are enumerated structurally from nodeFieldSetters rather
	// than listed in prose: an OBJECT schema with no declared properties is
	// rejected by some Gemini schema validators, and all five declarations go
	// up in one tools array, so a rejection here would fail every build call.
	fieldProperties := make(map[string]any, len(nodeFieldSetters))
	for k := range nodeFieldSetters {
		fieldProperties[k] = map[string]any{"type": "string"}
	}
	fieldsSchema := map[string]any{
		"type":        "OBJECT",
		"description": "Extra node fields, all optional strings. keyMode is either \"byok\" or \"platform\" -- a new Provider node defaults to \"platform\" already, only set this to \"byok\" if the user specifically asks to use their own API key.",
		"properties":  fieldProperties,
	}
	return []funcDecl{
		{
			Name:        "add_node",
			Description: "Add a new node to the workflow canvas.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"type":     map[string]any{"type": "string", "description": "One of: trigger, agent, provider, tool, tool402, action, end, tendril."},
					"template": map[string]any{"type": "string", "description": "Template id within the type, e.g. \"chat\" for trigger, \"gemini\" for provider, \"agent\" for agent, \"email\" for action."},
					"name":     map[string]any{"type": "string", "description": "Display name for the node."},
					"fields":   fieldsSchema,
				},
				"required": []string{"type", "template"},
			},
		},
		{
			Name:        "update_node",
			Description: "Update fields on an existing node.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"id":       map[string]any{"type": "string", "description": "Node id to update."},
					"name":     map[string]any{"type": "string"},
					"template": map[string]any{"type": "string"},
					"fields":   fieldsSchema,
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "remove_node",
			Description: "Remove a node and any edges connected to it.",
			Parameters: map[string]any{
				"type":       "OBJECT",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
				"required":   []string{"id"},
			},
		},
		{
			Name:        "add_edge",
			Description: "Connect two nodes. Use kind=\"attach\" with toPort=\"model\" or \"tools\" to attach a provider or tool to an agent; otherwise use kind=\"flow\" for the main execution path.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"from":   map[string]any{"type": "string"},
					"to":     map[string]any{"type": "string"},
					"kind":   map[string]any{"type": "string", "description": "flow or attach"},
					"toPort": map[string]any{"type": "string", "description": "model or tools, only for kind=attach"},
				},
				"required": []string{"from", "to"},
			},
		},
		{
			Name:        "remove_edge",
			Description: "Remove an edge by id.",
			Parameters: map[string]any{
				"type":       "OBJECT",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
				"required":   []string{"id"},
			},
		},
		{
			Name: "web_search",
			Description: "Search the live web and get back a grounded answer with its sources. " +
				"This is your general-purpose way of finding anything out. Use it whenever you need " +
				"information you do not already reliably have -- what a service's API looks like, which " +
				"model or version is current, a product's real specifications, how a format or protocol " +
				"works, whether something the user mentioned actually exists. You may call it several " +
				"times in a row: search, read, then search again to narrow down or confirm before you " +
				"act. Prefer searching over guessing. You can also use it to answer a question the user " +
				"asked without building anything -- not every turn has to edit the graph.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "What you want to find out, as a natural-language question. Ask for the specifics you actually need rather than a broad topic -- \"OpenWeatherMap current weather endpoint URL, required query parameters and auth header\" beats \"weather API\".",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

const buildAgentModel = "gemini-2.5-flash"

// The builder shares a loop with research, so it needs more rounds than the
// runtime agent's maxToolIterations: a few web_search calls to work out what
// it is building, then the node and edge calls to build it. Separate
// constant rather than raising maxToolIterations, which governs a user's own
// billed agent run and has nothing to do with this.
const maxBuildIterations = 25

const buildSystemPrompt = `You are the workflow builder for AgentMesh, a visual agent-workflow canvas.
You edit a workflow graph by calling the add_node, update_node, remove_node, add_edge, and remove_edge tools.
Node types and their templates:
- trigger: manual, chat, webhook, cron
- agent: agent, router, human
- provider: gemini, openai, anthropic, mistral, groq (attach to an agent's "model" port via an attach edge)
- tool: http, calc, datetime, websearch (attach to an agent's "tools" port via an attach edge). websearch answers
  a query grounded in a live Google Search via the platform's own Gemini key -- use it for anything needing
  current/real-world information, regardless of which provider the agent itself uses.
- tool402: no preset templates -- every x402 tool is a real, live endpoint the workflow owner supplies (a
  fictitious provider name like "tavily" or "firecrawl" is NOT wired to anything real). Only add one when the
  user gives you an actual endpoint URL; set node fields url/endpoint accordingly and pick a short descriptive
  template label. Prefer websearch for search unless the user specifically wants a paid x402 data source.
- action: email, slack, db, discord, teams, google_chat, ntfy, telegram, github, notion, airtable, hubspot, trello, asana, clickup, jira, mailchimp, linear, todoist, gitlab, sentry, supabase, woocommerce, elevenlabs
- end: http, done
- tendril: tendril_topup, tendril_rent, tendril_run, tendril_release

You have web_search, your own live web search. It is YOUR research tool, for finding things out while you
work. It is not the same thing as the "websearch" tool node you can add to a workflow for the finished agent
to use at run time -- adding that node is a separate decision.

Use it whenever you need to know something and are not confident you already do. You decide when; nobody has
to ask you to search. Typical reasons: what a service's API actually looks like, which model or version is
current, a product's real specifications, how some format or protocol works, whether something the user
mentioned exists at all. Search more than once when one query is not enough -- read what came back, then ask
a narrower question. Guessing and being wrong costs the user a broken workflow; searching costs a second.

Not every turn has to change the graph. If the user asks you a question, searching and answering it plainly
is a complete and correct turn.

When the workflow needs to call an API:
1. Search for the API first. Find its base URL and path, its HTTP method, how it authenticates, and what its
   request body and response look like.
2. Add a tool node with template "http", and set url and method from what you actually found. Never invent a
   URL and never adapt one you half-remember -- if the search did not turn up a real endpoint, say so in your
   reply instead of wiring a node that will 404. json_extract right after an http node is usually how you
   pull the one value you wanted out of the response.
3. Do NOT use a tool402 node for an API you found by searching. tool402 is for paid x402 endpoints, and only
   ever when the user hands you a real endpoint URL themselves.

Wiring rules -- these are enforced, an illegal edge is rejected and you must fix it:
- An attach edge always runs SOURCE -> AGENT, never the other way round: add_edge(from=<provider id>,
  to=<agent id>, kind="attach", toPort="model"). Writing from=<agent> to=<provider> is wrong and will
  be rejected.
- A provider attaches to the "model" port. A tool or tool402 attaches to the "tools" port. Nothing else
  attaches at all.
- A provider node is NEVER part of the flow chain -- it only ever attaches to an agent.
- Nothing flows into a trigger; a trigger is where the workflow starts.

A typical workflow: a trigger node, connected via a flow edge to an agent node, with a provider node attached to
the agent's "model" port and zero or more tool/tool402 nodes attached to its "tools" port, flowing on to an
action or end node. Every agent you add MUST end up with a provider attached to its "model" port -- an agent
without one cannot run. Make small, sensible workflows unless asked for something more elaborate. When you are
done making changes, reply with a short plain-text summary of what you built or changed -- do not call any more
tools once you're done.`

// BuildGraphResult is what a build-mode chat turn resolves to: a
// user-facing summary plus the graph after every requested tool call has
// been applied.
type BuildGraphResult struct {
	Reply string
	Graph models.WorkflowGraph
}

// BuildGraph runs a bounded tool-calling loop against the Gemini Flash
// meta-agent, letting it edit graph via the 5 graph_tool_decls tools until
// it responds with plain text instead of a function call (or the iteration
// cap is hit). graph should already be masked by the caller -- see
// handlers.BuildWorkflow's doc comment for why.
func BuildGraph(ctx context.Context, apiKey, userMessage string, graph models.WorkflowGraph) (BuildGraphResult, error) {
	apiURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent", geminiBaseURL, buildAgentModel)
	apiHeaders := map[string]string{"x-goog-api-key": apiKey}

	graphJSON, _ := json.Marshal(graph)
	contents := []map[string]any{
		{"role": "user", "parts": []map[string]any{{"text": fmt.Sprintf("Current graph:\n%s\n\nRequest: %s", graphJSON, userMessage)}}},
	}
	payload := map[string]any{
		"contents": contents,
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": buildSystemPrompt}},
		},
		"tools": []map[string]any{{"functionDeclarations": graphToolDecls()}},
	}

	auditRetried := false
	for iter := 0; iter < maxBuildIterations; iter++ {
		resp, err := postLLMJSON(ctx, apiURL, apiHeaders, payload)
		if err != nil {
			return BuildGraphResult{}, err
		}
		calls := extractGeminiFunctionCalls(resp)
		if len(calls) == 0 {
			text, err := extractGeminiText(resp)
			if err != nil {
				return BuildGraphResult{}, err
			}
			// Per-edge validation cannot see an agent that simply never got a
			// provider, or a node left wired to nothing -- those are made of
			// legal edges, or of none. Hand the findings back once and let the
			// model repair before it answers. Once only (auditRetried): if it
			// cannot fix the graph on a second pass it will not fix it on a
			// tenth, and the iteration budget is shared with real work.
			if !auditRetried {
				if findings := auditGraph(graph); len(findings) > 0 {
					auditRetried = true
					contents = append(contents,
						map[string]any{"role": "model", "parts": []map[string]any{{"text": text}}},
						map[string]any{"role": "user", "parts": []map[string]any{{"text": fmt.Sprintf(
							"Before you answer: the graph still has these problems.\n- %s\nFix them with the graph tools, then summarise.",
							strings.Join(findings, "\n- "))}}},
					)
					payload["contents"] = contents
					continue
				}
			}
			return BuildGraphResult{Reply: text, Graph: graph}, nil
		}

		modelParts := make([]map[string]any, len(calls))
		for i, c := range calls {
			modelParts[i] = map[string]any{"functionCall": map[string]any{"name": c.name, "args": c.args}}
		}
		contents = append(contents, map[string]any{"role": "model", "parts": modelParts})

		responseParts := make([]map[string]any, 0, len(calls))
		for _, c := range calls {
			// web_search is not a graph mutation, so it does not go through
			// applyGraphOp -- it reads the world instead of writing the graph,
			// and its response shape (answer + sources) is richer than the
			// one-line result string a graph op returns.
			if c.name == "web_search" {
				out, err := webSearch(ctx, argString(c.args, "query"), apiKey)
				payloadResp := map[string]any{}
				if err != nil {
					// The query text is the model's own, and webSearch's errors
					// are its own wrapped messages rather than a raw upstream
					// body, so this is safe to hand back -- and the model needs
					// to know the search failed rather than silently proceeding
					// as though it had returned nothing of interest.
					payloadResp["error"] = err.Error()
				} else {
					payloadResp["result"] = out
				}
				responseParts = append(responseParts, map[string]any{
					"functionResponse": map[string]any{
						"name":     c.name,
						"response": payloadResp,
					},
				})
				continue
			}
			result, err := applyGraphOp(&graph, c.name, c.args)
			if err != nil {
				result = "error: " + err.Error()
			}
			responseParts = append(responseParts, map[string]any{
				"functionResponse": map[string]any{
					"name":     c.name,
					"response": map[string]any{"result": result},
				},
			})
		}
		contents = append(contents, map[string]any{"role": "user", "parts": responseParts})
		payload["contents"] = contents
	}

	// Out of rounds. Return what was actually built instead of an error: the
	// graph has real nodes and edges on it by now, and an error return means
	// BuildWorkflow never reaches its save, so every one of them is lost --
	// the worst outcome available, and the likeliest one on exactly the
	// elaborate requests where the user cares most. Say plainly that it is
	// unfinished so the reply is not mistaken for a completed build.
	return BuildGraphResult{
		Reply: "I ran out of steps partway through this one. What I managed to build is on the canvas — " +
			"tell me what to finish and I'll carry on from there.",
		Graph: graph,
	}, nil
}
