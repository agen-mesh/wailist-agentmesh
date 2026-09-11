package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
	"github.com/agentmesh/backend/internal/models"
)

// graphNodeTypes is every node type the builder may create: each type in the
// node catalog, plus tool402. An x402 endpoint is deliberately not a catalog
// template -- every one is a different real endpoint charging real money --
// so it is added from the Bazaar catalog (add_x402_node) or from a URL the
// user supplies, never picked from a fixed list.
var graphNodeTypes = func() map[string]bool {
	m := map[string]bool{"tool402": true}
	for _, t := range NodeCatalogData().Types {
		m[t.Type] = true
	}
	return m
}()

var graphEdgeKinds = map[string]bool{"flow": true, "attach": true}

// tool402FieldKeys are the settable fields of a hand-specified x402 node --
// the one node type the catalog does not describe.
//
// "endpoint", never "url": tool402 calls node.Endpoint and does not read
// node.URL at all, so accepting url here would store the address somewhere
// no run ever looks -- which is exactly what the old prompt's "set url /
// endpoint" produced.
var tool402FieldKeys = []string{"endpoint", "method", "price", "unit", "provider", "description"}

// secretConfigKeys are credentials the builder may never set, whatever the
// catalog says, through either "fields" or "config". Named explicitly so the
// rejection can say what to do instead, rather than the generic "not a
// setting" that would leave the model retrying variations of the same call.
//
// apiKey and emailApiKey are here because they are top-level encrypted
// properties: the model only ever sees the "__enc__" sentinel
// (redactNodesForBuildAgent), so anything it wrote would be invented, and
// encryptField would encrypt it over the top of the user's real credential.
var secretConfigKeys = map[string]bool{
	"httpHeadersJSON": true, "httpBasicUser": true, "httpBasicPass": true,
	"apiKey": true, "emailApiKey": true, "tendrilLeaseToken": true,
	"apiToken": true, "authorization": true, "token": true,
}

// scheduleTriggerNames are what a model reaches for when asked for a
// recurring workflow. None exists; the rejection explains what does.
var scheduleTriggerNames = map[string]bool{
	"cron": true, "schedule": true, "scheduled": true, "interval": true, "timer": true,
}

// nodeStringFields maps each top-level string property of WorkflowNode, by
// its json name, to its struct index. The catalog decides WHICH of these a
// given template may set; this only performs the assignment, so a new
// catalog field needs no hand-written setter.
var nodeStringFields = func() map[string]int {
	out := map[string]int{}
	rt := reflect.TypeOf(models.WorkflowNode{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.String {
			continue
		}
		if name := strings.Split(f.Tag.Get("json"), ",")[0]; name != "" && name != "-" {
			out[name] = i
		}
	}
	return out
}()

func setNodeField(n *models.WorkflowNode, key, v string) {
	if idx, ok := nodeStringFields[key]; ok {
		reflect.ValueOf(n).Elem().Field(idx).SetString(v)
	}
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

// nodeRules is what one node may have set on it: its catalog template (zero
// for tool402 and for a custom node with no template), and the keys it
// accepts through "fields" and through "config".
type nodeRules struct {
	nodeType string
	tpl      CatalogTemplate
	fields   []string
	config   []string
}

func typeNames() string {
	var out []string
	for t := range graphNodeTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// rulesFor returns the rules for a node of this type and template, or an
// error that names the real options -- the model can only correct a mistake
// it can see the right answer to.
func rulesFor(nodeType, template string) (nodeRules, error) {
	if nodeType == "tool402" {
		return nodeRules{nodeType: nodeType, fields: tool402FieldKeys}, nil
	}
	tpl, ok := catalogTemplate(nodeType, template)
	if !ok {
		t, _ := catalogType(nodeType)
		ids := make([]string, 0, len(t.Templates))
		for _, x := range t.Templates {
			ids = append(ids, x.ID)
		}
		msg := fmt.Sprintf("%s has no template %q; its templates are: %s", nodeType, template, strings.Join(ids, ", "))
		if nodeType == "trigger" && scheduleTriggerNames[template] {
			msg += ". There is no schedule or cron trigger: use a manual trigger, and tell the user to deploy the workflow and set the timetable from its Schedule option on the Workflows page (a 5-field cron expression, in UTC)"
		}
		return nodeRules{}, fmt.Errorf("%s", msg)
	}
	fields := tpl.keysWhere("field")
	if !slices.Contains(fields, "description") {
		fields = append(fields, "description")
	}
	return nodeRules{nodeType: nodeType, tpl: tpl, fields: fields, config: tpl.keysWhere("config")}, nil
}

// credentialError says the key is the user's to supply, and where they get it.
func (r nodeRules) credentialError(k string) error {
	msg := fmt.Sprintf(
		"%q holds a credential and cannot be set by you -- credentials are never sent to you and you must not invent one; instead set the node's description to name exactly which credential the user has to add in the Inspector, and say so in your reply",
		k)
	if f, ok := r.tpl.field(k); ok && f.Where == "connection" {
		msg = fmt.Sprintf("%q is an account the user links in the Inspector (%s); you cannot set it -- say in your reply that they need to connect it", k, f.Label)
	}
	if r.tpl.AuthDocURL != "" {
		msg += fmt.Sprintf(" (they get it from %s)", r.tpl.AuthDocURL)
	}
	return fmt.Errorf("%s", msg)
}

// parse reads and validates the string map under args[param] ("fields" or
// "config") against the keys this node accepts there.
func (r nodeRules) parse(args map[string]any, param string, allowed []string) (map[string]string, error) {
	out := map[string]string{}
	raw, ok := args[param].(map[string]any)
	if !ok {
		return out, nil
	}
	for k, v := range raw {
		if secretConfigKeys[k] {
			return nil, r.credentialError(k)
		}
		if f, ok := r.tpl.field(k); ok && (f.Where == "secret" || f.Where == "connection") {
			return nil, r.credentialError(k)
		}
		if !slices.Contains(allowed, k) {
			have := "none"
			if len(allowed) > 0 {
				have = strings.Join(allowed, ", ")
			}
			label := r.nodeType
			if r.tpl.ID != "" {
				label += "/" + r.tpl.ID
			}
			hint := ""
			if f, ok := r.tpl.field(k); ok {
				hint = fmt.Sprintf(" (%q is a %s key, not a %s key)", k, f.Where, param)
			}
			return nil, fmt.Errorf("%s has no %s key %q%s; its %s keys are: %s -- call describe_node for details", label, param, k, hint, param, have)
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s value %q must be a string, got %T", param, k, v)
		}
		if err := r.validateValue(k, s); err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
}

// validateValue catches values that are the right key but a wrong shape --
// each one a mistake a live build actually made, which fails only later, at
// run time, where nobody is watching. Rejecting here puts the fix in front of
// the model while it is still building.
func (r nodeRules) validateValue(k, v string) error {
	switch k {
	case "keyMode":
		// The platform key needs nothing from the user. A live build set
		// byok unprompted and then asked the user for a Gemini key; someone
		// who does want their own key pastes it in the Inspector, which is
		// where the mode toggle is too.
		if v != "platform" {
			return fmt.Errorf(`keyMode %q cannot be set by you: leave it unset (a provider uses the platform key by default and needs nothing from the user); a user who wants their own key switches the key mode in the Inspector, where they paste it`, v)
		}
	case "model":
		// Only a model the engine prices for this provider. A live build
		// chose "gemini-pro", a retired name that fails the call.
		known := modelTiers[r.tpl.ID]
		if len(known) == 0 || v == "" {
			return nil
		}
		if _, ok := known[v]; !ok {
			names := make([]string, 0, len(known))
			for m := range known {
				names = append(names, m)
			}
			sort.Strings(names)
			msg := fmt.Sprintf("model %q is not a %s model this platform supports; use one of: %s", v, r.tpl.ID, strings.Join(names, ", "))
			if def := r.tpl.Presets["model"]; def != "" {
				msg += fmt.Sprintf(" -- or leave model unset for the default, %s", def)
			}
			return fmt.Errorf("%s", msg)
		}
	case "jsonPath":
		// walkPath splits on dots and nothing else, so JSONPath syntax (the
		// "$.data[0].lastPrice" a live build wrote) fails on the "$" segment.
		if strings.ContainsAny(v, "$[]") {
			return fmt.Errorf("jsonPath %q uses JSONPath syntax, which json_extract does not read: it takes a plain dot path where numbers index arrays -- use %q", v, toDotPath(v))
		}
	}
	return nil
}

// anyTemplateRef finds every {{ ... }} in a value -- deliberately looser than
// the engine's templateRef, so malformed references are caught too.
var anyTemplateRef = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// templatePath matches a dotted field path after "result." or "node.<id>.".
var templatePath = regexp.MustCompile(`^[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]+)*$`)

// validateTemplateRefs checks every {{ ... }} reference in values against
// what the engine actually resolves (resolveTemplate / ExpandState). An
// unsupported reference is not an error at run time -- it is left in the
// output verbatim -- which is why it has to be caught while building: a
// live build wrote {{n_<id>.output}}, and the step would have produced
// literal braces instead of prices.
func validateTemplateRefs(graph *models.WorkflowGraph, values map[string]string) error {
	forms := "{{ result }}, {{ result.field }}, {{ input }}, {{ node.<id> }}, {{ node.<id>.field }} or {{ state.key }}"
	for key, v := range values {
		for _, m := range anyTemplateRef.FindAllStringSubmatch(v, -1) {
			ref := m[1]
			switch {
			case ref == "result" || ref == "input":
				continue
			case strings.HasPrefix(ref, "result.") && templatePath.MatchString(ref[len("result."):]):
				continue
			case stateRef.MatchString(m[0]):
				continue
			case strings.HasPrefix(ref, "param:"), strings.HasPrefix(ref, "file:"),
				strings.HasPrefix(ref, "fileName:"), strings.HasPrefix(ref, "fileType:"):
				continue // tool402 body placeholders, expanded separately
			case strings.HasPrefix(ref, "node."):
				id, path, _ := strings.Cut(ref[len("node."):], ".")
				if _, ok := findGraphNode(graph, id); !ok {
					return fmt.Errorf("%s: {{ %s }} refers to node %q, which is not in the graph -- use the id add_node returned", key, ref, id)
				}
				if path == "output" {
					return fmt.Errorf("%s: {{ %s }} -- a node's output is {{ node.%s }} itself; \".output\" would look for a field named output", key, ref, id)
				}
				if path != "" && !templatePath.MatchString(path) {
					return fmt.Errorf("%s: {{ %s }} has an invalid field path; use a dot path such as {{ node.%s.data.0.price }}", key, ref, id)
				}
				continue
			}
			if id, _, _ := strings.Cut(ref, "."); id != "" {
				if _, ok := findGraphNode(graph, id); ok {
					return fmt.Errorf("%s: {{%s}} is not a reference the engine resolves -- it would be left in the text as-is. Write {{ node.%s }} for that node's output, or {{ node.%s.field }} for one field of it", key, ref, id, id)
				}
			}
			return fmt.Errorf("%s: {{%s}} is not a reference the engine resolves -- it would be left in the text as-is. Use %s", key, ref, forms)
		}
	}
	return nil
}

// toDotPath converts JSONPath-style syntax into the dot path walkPath reads:
// "$.data[0].lastPrice" -> "data.0.lastPrice", "$['data'][2]" -> "data.2".
func toDotPath(p string) string {
	p = strings.TrimPrefix(p, "$")
	p = strings.NewReplacer("['", ".", "']", "", `["`, ".", `"]`, "", "[", ".", "]", "").Replace(p)
	for strings.Contains(p, "..") {
		p = strings.ReplaceAll(p, "..", ".")
	}
	return strings.Trim(p, ".")
}

// userSuppliedKeys lists the credentials and connections a node of this type
// can take from the user. A provider's apiKey is left out: it only applies
// in byok mode, which the builder can never set, so a builder-made provider
// never needs one -- and a live build that saw it listed told the user to
// paste a Gemini key for a provider running on the platform key.
func userSuppliedKeys(nodeType string, tpl CatalogTemplate) []string {
	var keys []string
	for _, f := range tpl.Fields {
		if f.Where != "secret" && f.Where != "connection" {
			continue
		}
		if nodeType == "provider" && f.Key == "apiKey" {
			continue
		}
		keys = append(keys, f.Key)
	}
	return keys
}

// userSupplied lists the credentials and connections this node can take, so
// the tool result reminds the model to disclose the ones the workflow needs.
func (r nodeRules) userSupplied() string {
	keys := userSuppliedKeys(r.nodeType, r.tpl)
	if len(keys) == 0 {
		return ""
	}
	s := " -- the user supplies its credentials (you cannot): " + strings.Join(keys, ", ")
	if r.tpl.AuthDocURL != "" {
		s += " from " + r.tpl.AuthDocURL
	}
	return s + "; name the ones this workflow needs on the node's description and in your reply"
}

func addGraphNode(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	nodeType := argString(args, "type")
	if !graphNodeTypes[nodeType] {
		return "", fmt.Errorf("add_node: invalid type %q; valid types: %s", nodeType, typeNames())
	}
	template := argString(args, "template")
	rules, err := rulesFor(nodeType, template)
	if err != nil {
		return "", fmt.Errorf("add_node: %w", err)
	}
	fields, err := rules.parse(args, "fields", rules.fields)
	if err != nil {
		return "", err
	}
	cfg, err := rules.parse(args, "config", rules.config)
	if err != nil {
		return "", err
	}
	if err := validateTemplateRefs(graph, fields); err != nil {
		return "", err
	}
	if err := validateTemplateRefs(graph, cfg); err != nil {
		return "", err
	}
	id := newGraphID("n_")
	node := models.WorkflowNode{
		ID:       id,
		Type:     models.NodeType(nodeType),
		Template: template,
		Name:     argString(args, "name"),
		X:        80 + 240*float64(len(graph.Nodes)%4),
		Y:        120 + 160*float64(len(graph.Nodes)/4),
	}
	// Presets first, exactly as the palette applies them on drop: a state
	// node runs on stateOp and a Tendril node on tendrilAction, not on the
	// template, so without these the node silently does the wrong thing or
	// fails outright. The identity presets are not in the template's
	// "fields", so an explicit field below can only refine the defaults.
	for k, v := range rules.tpl.Presets {
		setNodeField(&node, k, v)
	}
	for k, v := range fields {
		setNodeField(&node, k, v)
	}
	if len(cfg) > 0 {
		node.Config = cfg
	}
	// Default a new Provider node to platform-key mode unless the model
	// explicitly chose "byok" -- resolveAPIKey (provider.go) treats any
	// KeyMode other than "platform" as BYOK and reads node.APIKey, which a
	// chat-built node never has. Deterministic here rather than relying on
	// prompt compliance -- this must hold every time, not most of the time.
	if node.Type == models.NodeTypeProvider && node.KeyMode == "" {
		node.KeyMode = "platform"
	}
	graph.Nodes = append(graph.Nodes, node)
	return fmt.Sprintf("added node %s (%s/%s)%s", id, nodeType, node.Template, rules.userSupplied()), nil
}

func updateGraphNode(graph *models.WorkflowGraph, args map[string]any) (string, error) {
	id := argString(args, "id")
	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		if n.ID != id {
			continue
		}
		template := n.Template
		newTemplate := argString(args, "template")
		if newTemplate != "" {
			template = newTemplate
		}
		rules, err := rulesFor(string(n.Type), template)
		if err != nil {
			// A node the user made from a palette "custom" item has no
			// template. It can still be renamed and described, but anything
			// else needs a real template first so there is something to
			// validate against.
			if newTemplate != "" {
				return "", fmt.Errorf("update_node: %w", err)
			}
			rules = nodeRules{nodeType: string(n.Type), fields: []string{"description"}}
		}
		fields, err := rules.parse(args, "fields", rules.fields)
		if err != nil {
			if rules.tpl.ID == "" && n.Type != models.NodeTypeTool402 {
				err = fmt.Errorf("%w (node %s has no template -- set one with update_node template=... before configuring it)", err, id)
			}
			return "", err
		}
		cfg, err := rules.parse(args, "config", rules.config)
		if err != nil {
			return "", err
		}
		if err := validateTemplateRefs(graph, fields); err != nil {
			return "", err
		}
		if err := validateTemplateRefs(graph, cfg); err != nil {
			return "", err
		}
		if newTemplate != "" && newTemplate != n.Template {
			n.Template = newTemplate
			for k, v := range rules.tpl.Presets {
				setNodeField(n, k, v)
			}
		}
		if name := argString(args, "name"); name != "" {
			n.Name = name
		}
		for k, v := range fields {
			setNodeField(n, k, v)
		}
		// Merge rather than replace: the user may have set a key by hand in
		// the Inspector, and an unrelated update from chat must not wipe it.
		if len(cfg) > 0 {
			if n.Config == nil {
				n.Config = map[string]string{}
			}
			for k, v := range cfg {
				n.Config[k] = v
			}
		}
		return fmt.Sprintf("updated node %s", id), nil
	}
	return "", fmt.Errorf("update_node: node %q not found", id)
}

// describeNode returns the full catalog entry for one template -- labels,
// hints, placeholders, presets and where credentials come from -- for when
// the compact list in the prompt is not enough to fill a setting correctly.
func describeNode(nodeType, template string) (string, error) {
	if nodeType == "tool402" {
		return "tool402 is an x402 endpoint, not a catalog template. Prefer add_x402_node with an id from search_x402; with a URL the user gave you, add_node type=tool402 fields: " + strings.Join(tool402FieldKeys, ", "), nil
	}
	rules, err := rulesFor(nodeType, template)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(rules.tpl)
	return string(out), nil
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

// catalogKeyUnion collects every settable key of one kind across the whole
// catalog, for the tool schemas below.
func catalogKeyUnion(where string, extra ...string) map[string]any {
	props := map[string]any{}
	for _, k := range extra {
		props[k] = map[string]any{"type": "string"}
	}
	for _, t := range NodeCatalogData().Types {
		for _, tpl := range t.Templates {
			for _, k := range tpl.keysWhere(where) {
				props[k] = map[string]any{"type": "string"}
			}
		}
	}
	return props
}

func graphToolDecls() []funcDecl {
	// The legal keys are enumerated structurally from the node catalog rather
	// than listed in prose: an OBJECT schema with no declared properties is
	// rejected by some Gemini schema validators, and every declaration goes
	// up in one tools array, so a rejection here would fail every build call.
	// This is the union across all templates; which keys a particular
	// template accepts is checked per call (rulesFor) and listed in the prompt.
	fieldsSchema := map[string]any{
		"type": "OBJECT",
		"description": "Top-level node fields, all strings. Only the keys listed for this template in the node catalog are accepted. " +
			"keyMode is \"platform\" (the default for a new provider) or \"byok\" -- only use byok if the user asks to use their own key.",
		"properties": catalogKeyUnion("field", tool402FieldKeys...),
	}
	configSchema := map[string]any{
		"type": "OBJECT",
		"description": "Non-secret node settings (node.config), all strings. Only the keys listed for this template in the node catalog are accepted. " +
			"Credentials are never settable -- name them on the node's description instead.",
		"properties": catalogKeyUnion("config"),
	}
	return []funcDecl{
		{
			Name:        "add_node",
			Description: "Add a new node to the workflow canvas.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"type":     map[string]any{"type": "string", "description": "A node type from the node catalog, or tool402."},
					"template": map[string]any{"type": "string", "description": "A template id of that type from the node catalog (for tool402, any short label)."},
					"name":     map[string]any{"type": "string", "description": "Display name for the node."},
					"fields":   fieldsSchema,
					"config":   configSchema,
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
					"config":   configSchema,
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
			Name:        "describe_node",
			Description: "Get the full detail of one node template from the catalog: every setting with its label, hint and example, its presets, and where its credentials come from. Call this when the compact catalog list is not enough to fill a setting correctly.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"type":     map[string]any{"type": "string"},
					"template": map[string]any{"type": "string"},
				},
				"required": []string{"type", "template"},
			},
		},
		{
			Name: "search_x402",
			Description: "Search the x402 Bazaar: real, pay-per-call endpoints (data feeds, AI services, tools) the platform can pay for. " +
				"Returns ids, what each does, its inputs, an example output, how often it has actually been paid, and its full per-call cost. " +
				"Use short keywords (\"stock prices\", \"weather forecast\").",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "A few keywords describing what the endpoint should do."},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "add_x402_node",
			Description: "Add an x402 endpoint from search_x402 results as a tool402 node. The endpoint, method, price and inputs are filled in from the catalog -- pass only the id. " +
				"Attach it to an agent's \"tools\" port so the agent supplies its inputs per call.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"id":   map[string]any{"type": "string", "description": "An id exactly as search_x402 returned it."},
					"name": map[string]any{"type": "string", "description": "Optional display name for the node."},
				},
				"required": []string{"id"},
			},
		},
		{
			Name: "fetch_url",
			Description: "Call a URL with a plain GET, exactly as a workflow http step would at run time, and see the status and body. " +
				"Call it on every API before you wire an http node to it: it tells you whether the endpoint answers a server at all, " +
				"and shows the real JSON you must read any jsonPath from.",
			Parameters: map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"url": map[string]any{"type": "string", "description": "The exact URL the http node will call."},
				},
				"required": []string{"url"},
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

// defaultBuildTimeBudget keeps a build inside the frontend's proxy window
// (next.config.ts proxyTimeout: 120s). A build that ran past it had its
// request cut off, its context cancelled mid-loop, and everything it had
// built discarded -- research tools (web_search, fetch_url) make long builds
// common enough that this has to be a hard property, not a hope.
const defaultBuildTimeBudget = 100 * time.Second

// buildSystemPrompt is the builder's standing instructions with the node
// catalog spliced in. A var built at init rather than a hand-typed const:
// the old hand-typed node list drifted (a cron trigger that never existed,
// 4 of 12 tools, 24 of 42 connectors, no settings at all), and generating it
// from the same catalog that validates every tool call means what the model
// is told and what it is allowed can never disagree.
var buildSystemPrompt = strings.Replace(builderPromptTemplate, "{{NODE_CATALOG}}", catalogPromptSection(), 1)

// catalogPromptSection renders the catalog compactly: one line per template
// with the keys the model may set, the credentials it must leave to the
// user, and any NOTE about real behaviour. Full detail (labels, hints,
// examples) is one describe_node call away, which keeps this short enough to
// resend on every round of the tool loop.
func catalogPromptSection() string {
	var b strings.Builder
	for _, t := range NodeCatalogData().Types {
		fmt.Fprintf(&b, "%s -- %s\n", t.Type, t.Desc)
		for _, tpl := range t.Templates {
			fmt.Fprintf(&b, "  %s %q -- %s", tpl.ID, tpl.Name, tpl.Desc)
			if f := tpl.keysWithExamples("field"); len(f) > 0 {
				fmt.Fprintf(&b, "; fields: %s", strings.Join(f, ", "))
			}
			if c := tpl.keysWithExamples("config"); len(c) > 0 {
				fmt.Fprintf(&b, "; config: %s", strings.Join(c, ", "))
			}
			user := userSuppliedKeys(t.Type, tpl)
			if tpl.OAuthProvider != "" {
				user = append(user, "or connect "+tpl.OAuthProvider)
			}
			if len(user) > 0 {
				fmt.Fprintf(&b, "; user supplies: %s", strings.Join(user, ", "))
				if tpl.AuthDocURL != "" {
					fmt.Fprintf(&b, " (from %s)", tpl.AuthDocURL)
				}
			}
			if tpl.Note != "" {
				fmt.Fprintf(&b, "; NOTE: %s", tpl.Note)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

const builderPromptTemplate = `You are the workflow builder for AgentMesh, a visual agent-workflow canvas.
You edit a workflow graph with the add_node, update_node, remove_node, add_edge and remove_edge tools.
Work in as few rounds as you can -- every round is a full model call, and a build has a time limit. Put all
the independent tool calls you can in the same turn: all your research at once, then all add_node calls
together, then all add_edge calls together (edges need the node ids add_node returned).

Use ONLY the node types, templates and settings in the NODE CATALOG below. It is generated from the canvas
and the engine, so anything not in it does not exist and will be rejected. For each template it lists the
keys you may set: "fields" (top-level, via add_node fields) and "config" (node settings, via add_node
config); "description" is always settable. Keys under "user supplies" are credentials or linked accounts:
you cannot set them, so name the ones this workflow needs on the node's description and in your reply,
together with where the user gets them. Read every NOTE -- it describes what the node really does, which
can differ from its name. Presets (a state node's operation, a provider's default model) are applied for
you. Call describe_node for a template's full detail whenever you need the exact format a setting expects;
configure every node you add so it can actually run, instead of describing settings you did not set.

Designing the flow: every flow step receives the previous step's output. Add an agent only where language or
judgement is needed (summarising, deciding, writing a message) -- each agent call costs credits. A pure data
job needs no agent and no provider: trigger -> http -> json_extract -> state -> end runs as it is. When an
agent is needed, put it AFTER the data steps it should read, e.g. trigger -> http -> json_extract -> agent
-> slack -> end. A tool attached to an agent's "tools" port is called BY
the agent and its result goes back to the agent, not to the next flow step -- so never put json_extract,
xml, html_extract or markdown after an agent expecting fetched data; an agent outputs prose. Leave a
provider's keyMode and model unset unless the user asks for a specific model: the defaults run on the
platform key and need nothing from the user. A public API you found may still block server requests or
need headers, so say in your reply that its step should be checked with a manual run.

Schedules: there is no schedule or cron trigger. For anything that should run on a timetable (daily,
hourly, every Monday, ...), use a manual trigger, then tell the user to deploy the workflow and set the
timetable from its Schedule option on the Workflows page. It takes a standard 5-field cron expression
evaluated in UTC -- give them the exact expression, converted from their time zone (09:00 IST every day is
"30 3 * * *"). Never claim you set a schedule yourself.

x402 endpoints (node type tool402): real pay-per-call services from the x402 Bazaar. Every call costs the user
the endpoint's price PLUS a 1.50 USD AgentMesh fee -- usually far more than the endpoint itself -- and an agent
may call an attached tool several times in one run. So reach for x402 only when the data or service is not
available for free: first consider an http tool node on a free public API, a no-key connector (coingecko,
openweathermap, hackernews, rss, ...), or websearch. When x402 is the right answer:
1. search_x402 with a few keywords. Prefer entries with a high timesPaid -- they are proven to work.
2. add_x402_node with the id it returned. Never add a tool402 node by typing an endpoint yourself.
3. Attach it to an agent's "tools" port so the agent fills its inputs, and state the full per-call cost
   (endpoint price + AgentMesh fee) in your reply.
Only if the user hands you an x402 endpoint URL that is not in the catalog, add it with add_node type=tool402,
fields endpoint and method, and tell them to press Discover in the Inspector to confirm its live price.
Never invent a provider: a name like "tavily" or "firecrawl" that is not in search results is wired to nothing.

NODE CATALOG
{{NODE_CATALOG}}

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
   Research is limited: a build has a time limit, so use at most three research calls (web_search, fetch_url)
   before you start adding nodes.
   For live information -- market or stock prices, exchange rates, news, sports, weather anywhere -- free APIs
   are usually blocked or rate-limited for servers. Prefer the websearch tool node attached to the agent's
   "tools" port: the agent looks the information up live on every run, and there is no API to break. Use an
   http node only when you have a specific API you can verify.
2. Call fetch_url on the exact URL before wiring anything. If it does not answer status 200 with the data you
   need, the workflow's http step will fail the same way -- do not wire it; find another source, or tell the
   user you could not find one that works. Never invent a URL or adapt one you half-remember.
3. Add a tool node with template "http" set to that URL, then json_extract with a jsonPath read from the body
   fetch_url actually returned -- never from memory.
4. Wire every step you add: each flow step needs a flow edge INTO it from the step whose output it reads.
   A step nothing flows into is not skipped -- the engine runs it first, on an empty input, and the run fails.
   To combine several values, reference each earlier step as {{ node.<id> }} (or {{ node.<id>.field }}),
   using the ids add_node returned.
5. Do NOT use a tool402 node for an API you found by searching. tool402 is for paid x402 endpoints, and only
   ever when the user hands you a real endpoint URL themselves.

Credentials: you are never shown a user's API keys and you must never invent one. If an API you wired needs
a key, a token or a basic-auth login, set that node's description to name exactly what is needed and where it
goes -- for example "needs an OpenWeatherMap API key in the appid query parameter" -- and say the same thing
in your reply so the user knows to add it in the Inspector. A node with a fabricated key looks configured and
is not, which is worse than one that is plainly incomplete.

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

// BuildTurn is one prior turn of the builder conversation, replayed into the
// model's context so a follow-up ("use the specs I gave you") has something
// to refer to. Role is "user" or "model".
//
// Declared here rather than reusing db.BuildMessage so this package keeps its
// existing independence from the database package -- the handler converts at
// the boundary.
type BuildTurn struct {
	Role string
	Text string
}

// BuildGraphResult is what a build-mode chat turn resolves to: a
// user-facing summary plus the graph after every requested tool call has
// been applied.
type BuildGraphResult struct {
	Reply string
	Graph models.WorkflowGraph
}

// BuildRequest is one build-mode chat turn.
type BuildRequest struct {
	// APIKey is the platform Gemini key the builder (and web_search) run on.
	APIKey string
	// Message is what the user just typed.
	Message string
	// Graph is the current graph, already masked by the caller -- see
	// handlers.BuildWorkflow's doc comment for why.
	Graph models.WorkflowGraph
	// History is the prior conversation, oldest first; nil is a cold turn.
	History []BuildTurn
	// X402Catalog loads the x402 Bazaar catalog. Called at most once per
	// build and only if the model searches for an endpoint; nil makes
	// search_x402/add_x402_node report the catalog as unavailable.
	X402Catalog func(ctx context.Context) ([]bazaar.Resource, error)
	// TimeBudget bounds the whole build; zero means defaultBuildTimeBudget.
	TimeBudget time.Duration
	// TraceID, when set, logs one line per round (tool names, elapsed time)
	// under that id -- without it a slow build cannot be diagnosed.
	TraceID string
}

// BuildGraph runs a bounded tool-calling loop against the Gemini Flash
// meta-agent, letting it edit the graph via the graphToolDecls tools, look
// things up with web_search/describe_node/search_x402, until it responds
// with plain text instead of a function call. Running out of rounds returns
// the partial graph rather than an error -- see the tail of the loop.
func BuildGraph(ctx context.Context, req BuildRequest) (BuildGraphResult, error) {
	apiKey, userMessage, graph, history := req.APIKey, req.Message, req.Graph, req.History
	x402 := newX402Session(req.X402Catalog)
	// probed caches fetchURL results per url for this build, so the model's
	// own fetch_url and the automatic check on add_node share one request.
	probed := map[string]string{}

	// Two limits. The context ends at the budget, so a model call or fetch
	// still in flight cannot outlive it; and no new round starts once three
	// quarters of it are gone, since one round (a model call plus any
	// fetch_url or web_search it asks for) can take several seconds and has
	// to finish inside the budget. Either way the partial graph is returned
	// -- like the round cap -- so nothing already built is thrown away.
	budget := req.TimeBudget
	if budget <= 0 {
		budget = defaultBuildTimeBudget
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	ranOutOfTime := func() BuildGraphResult {
		if req.TraceID != "" {
			log.Printf("build %s: stopped at the time budget after %.1fs", req.TraceID, time.Since(started).Seconds())
		}
		return BuildGraphResult{
			Reply: "I ran out of time partway through this one. What I built so far is on the canvas — " +
				"tell me what to finish and I'll carry on from there.",
			Graph: graph,
		}
	}
	apiURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent", geminiBaseURL, buildAgentModel)
	apiHeaders := map[string]string{"x-goog-api-key": apiKey}

	graphJSON, _ := json.Marshal(graph)
	// Prior turns first, then the current one carrying a FRESH graph
	// snapshot. The snapshot rides with the newest turn on purpose: the graph
	// changes between turns, and replaying an old one would leave the model
	// reasoning about nodes that have since been renamed or removed.
	contents := make([]map[string]any, 0, len(history)+1)
	for _, h := range history {
		// Gemini rejects any role other than user/model, and one bad row
		// replayed here would fail every build on this workflow from then on.
		if h.Role != "user" && h.Role != "model" {
			continue
		}
		if strings.TrimSpace(h.Text) == "" {
			continue
		}
		contents = append(contents, map[string]any{
			"role":  h.Role,
			"parts": []map[string]any{{"text": h.Text}},
		})
	}
	contents = append(contents, map[string]any{
		"role":  "user",
		"parts": []map[string]any{{"text": fmt.Sprintf("Current graph:\n%s\n\nRequest: %s", graphJSON, userMessage)}},
	})
	payload := map[string]any{
		"contents": contents,
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": buildSystemPrompt}},
		},
		"tools": []map[string]any{{"functionDeclarations": graphToolDecls()}},
	}

	auditRetried := false
	for iter := 0; iter < maxBuildIterations; iter++ {
		if iter > 0 && time.Since(started) > budget*3/4 {
			return ranOutOfTime(), nil
		}
		resp, err := postLLMJSON(ctx, apiURL, apiHeaders, payload)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ranOutOfTime(), nil
			}
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
							"Before you answer: the graph still has these problems.\n- %s\nFix them with the graph tools. Then reply to the user with a summary of the finished workflow as a whole -- do not mention these problems or the fixes, which the user never saw.",
							strings.Join(findings, "\n- "))}}},
					)
					payload["contents"] = contents
					continue
				}
			}
			return BuildGraphResult{Reply: text, Graph: graph}, nil
		}

		modelParts := make([]map[string]any, len(calls))
		names := make([]string, len(calls))
		for i, c := range calls {
			modelParts[i] = map[string]any{"functionCall": map[string]any{"name": c.name, "args": c.args}}
			names[i] = c.name
		}
		if req.TraceID != "" {
			log.Printf("build %s: round %d at %.1fs: %s", req.TraceID, iter+1, time.Since(started).Seconds(), strings.Join(names, ", "))
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
			// The x402 tools need the request's catalog loader, which a plain
			// graph op has no access to. add_x402_node does edit the graph,
			// but only ever from a catalog entry -- never from model-typed
			// URL or price fields.
			if c.name == "search_x402" || c.name == "add_x402_node" {
				var out string
				var err error
				if c.name == "search_x402" {
					out, err = x402.search(ctx, c.args)
				} else {
					out, err = x402.add(ctx, &graph, c.args)
				}
				if err != nil {
					out = "error: " + err.Error()
				}
				responseParts = append(responseParts, map[string]any{
					"functionResponse": map[string]any{
						"name":     c.name,
						"response": map[string]any{"result": out},
					},
				})
				continue
			}
			// fetch_url reads the world, never the graph.
			if c.name == "fetch_url" {
				responseParts = append(responseParts, map[string]any{
					"functionResponse": map[string]any{
						"name": c.name,
						"response": map[string]any{"result": func() string {
							u := strings.TrimSpace(argString(c.args, "url"))
							if r, ok := probed[u]; ok {
								return r
							}
							r := fetchURL(ctx, u)
							probed[u] = r
							return r
						}()},
					},
				})
				continue
			}
			// describe_node reads the catalog; like web_search it never edits
			// the graph, so it stays out of applyGraphOp.
			if c.name == "describe_node" {
				out, err := describeNode(argString(c.args, "type"), argString(c.args, "template"))
				if err != nil {
					out = "error: " + err.Error()
				}
				responseParts = append(responseParts, map[string]any{
					"functionResponse": map[string]any{
						"name":     c.name,
						"response": map[string]any{"result": out},
					},
				})
				continue
			}
			// An http node's url is checked before the node is added: a live
			// build wired an address it had never fetched and the run 404'd.
			// The prompt asking the model to verify first was not enough, so
			// the builder calls the url itself -- through the same client a
			// run uses -- and refuses one a run would fail on.
			probeNote := ""
			if url := httpNodeURLChange(&graph, c.name, c.args); url != "" {
				probe, seen := probed[url]
				if !seen {
					probe = fetchURL(ctx, url)
					probed[url] = probe
				}
				refuse, note := judgeProbe(url, probe)
				if refuse != "" {
					responseParts = append(responseParts, map[string]any{
						"functionResponse": map[string]any{
							"name":     c.name,
							"response": map[string]any{"result": "error: " + refuse},
						},
					})
					continue
				}
				probeNote = note
			}
			result, err := applyGraphOp(&graph, c.name, c.args)
			if err != nil {
				result = "error: " + err.Error()
			} else {
				result += probeNote
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
