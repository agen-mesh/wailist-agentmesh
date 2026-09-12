package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// Build progress: what the chat shows while the builder works, instead of a
// bare spinner -- "Searched the web for ...", "Added HTTP Request ...",
// "Couldn't add ... (HTTP 404)", one line per tool call, like a coding
// agent's transcript. Labels are written for the person watching, not for
// the model.

// BuildStep is one finished tool call.
type BuildStep struct {
	// Kind groups steps for display: search, check, node, edge, x402, look.
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status"` // "done" or "error"
	Detail string `json:"detail,omitempty"`
}

// BuildProgress is a snapshot: every step finished so far, and what is
// happening right now ("" when nothing is in flight).
type BuildProgress struct {
	Steps   []BuildStep `json:"steps"`
	Current string      `json:"current,omitempty"`
}

type progressTracker struct {
	on      func(BuildProgress)
	steps   []BuildStep
	current string
}

func (p *progressTracker) working(label string) {
	p.current = label
	p.emit()
}

func (p *progressTracker) finished(s BuildStep) {
	p.steps = append(p.steps, s)
	p.current = ""
	p.emit()
}

func (p *progressTracker) idle() {
	p.current = ""
	p.emit()
}

func (p *progressTracker) emit() {
	if p.on == nil {
		return
	}
	// A copy: the receiver may hold on to the snapshot while steps grows.
	p.on(BuildProgress{Steps: append([]BuildStep(nil), p.steps...), Current: p.current})
}

// runBuildCall executes one tool call from the model and returns the
// functionResponse payload for it. Every tool's failure comes back as data
// ({"result": "error: ..."} or {"error": ...}), never as a Go error, because
// the model needs to see it and try something else.
// graphMutations are the calls that change the graph, and so invalidate the
// last test run.
var graphMutations = map[string]bool{
	"add_node": true, "update_node": true, "remove_node": true,
	"add_edge": true, "remove_edge": true, "add_x402_node": true,
}

// maxTestRuns bounds the test runs one build may make, however often the
// model asks: each one really calls the workflow's agents and sources.
const maxTestRuns = 5

// testTracker remembers whether the graph, as it now stands, has been
// test-run and what that run produced.
type testTracker struct {
	run func(ctx context.Context, graph models.WorkflowGraph, input string) DryRunResult
	// dirty is set when this build changes the graph and cleared by a test
	// run. It starts false: a turn that changes nothing (a question, a
	// clarification) has nothing new to test.
	dirty bool
	last  *DryRunResult
	// rounds counts the times the gate sent the model back; runs counts
	// the test runs made.
	rounds int
	runs   int
}

func runBuildCall(ctx context.Context, graph *models.WorkflowGraph, c geminiFuncCall, apiKey string, x402 *x402Session, probed map[string]string, tester *testTracker) map[string]any {
	response := dispatchBuildCall(ctx, graph, c, apiKey, x402, probed, tester)
	if graphMutations[c.name] {
		if text, _ := response["result"].(string); !strings.HasPrefix(text, "error: ") {
			tester.dirty = true
		}
	}
	return response
}

func dispatchBuildCall(ctx context.Context, graph *models.WorkflowGraph, c geminiFuncCall, apiKey string, x402 *x402Session, probed map[string]string, tester *testTracker) map[string]any {
	switch c.name {
	case "test_run":
		if tester.run == nil {
			return map[string]any{"result": "error: test runs are not available here"}
		}
		if tester.runs >= maxTestRuns {
			return map[string]any{"result": fmt.Sprintf("error: this build has already used its %d test runs. Reply to the user now, and say plainly whether the last test produced the answer they asked for.", maxTestRuns)}
		}
		tester.runs++
		res := tester.run(ctx, *graph, argString(c.args, "input"))
		tester.last, tester.dirty = &res, false
		out, _ := json.Marshal(res)
		note := " -- Steps marked simulated were not performed (they would send, pay or write). "
		switch {
		case res.Failed:
			note += "The run FAILED: fix the step that failed and test again."
		case res.Empty:
			note += "Something returned NOTHING: find out why (a wrong id, a wrong path, a source with no data), fix it and test again."
		case res.Unverified:
			note += "Steps marked unverified could not be checked in a test (their input is simulated, or they need credits) -- that is not a fault, do not change the workflow because of it. Tell the user which steps went unchecked."
		default:
			note += "Check the answer really is what the user asked for before you reply, and quote it in your reply."
		}
		return map[string]any{"result": string(out) + note}

	case "web_search":
		// Not a graph mutation: it reads the world, and its answer + sources
		// shape is richer than a graph op's one-line result.
		out, err := webSearch(ctx, argString(c.args, "query"), apiKey)
		if err != nil {
			// The model needs to know the search failed, but not how:
			// webSearch's error wraps postLLMJSON's, which carries Gemini's raw
			// response body -- exactly what BuildWorkflow keeps out of the
			// chat. Logged here in full, reported as a plain failure.
			log.Printf("builder web_search failed: %v", err)
			return map[string]any{"error": "web search is unavailable right now"}
		}
		return map[string]any{"result": out}

	case "search_x402", "add_x402_node":
		// These need the request's catalog loader. add_x402_node edits the
		// graph, but only ever from a catalog entry -- never from model-typed
		// URL or price fields.
		var out string
		var err error
		if c.name == "search_x402" {
			out, err = x402.search(ctx, c.args)
		} else {
			out, err = x402.add(ctx, graph, c.args)
		}
		if err != nil {
			out = "error: " + err.Error()
		}
		return map[string]any{"result": out}

	case "fetch_url":
		u := strings.TrimSpace(argString(c.args, "url"))
		r, ok := probed[u]
		if !ok {
			r = fetchURL(ctx, u)
			probed[u] = r
		}
		return map[string]any{"result": r}

	case "describe_node":
		out, err := describeNode(argString(c.args, "type"), argString(c.args, "template"))
		if err != nil {
			out = "error: " + err.Error()
		}
		return map[string]any{"result": out}
	}

	// An http node's url is checked before the node is added: a live build
	// wired an address it had never fetched and the run 404'd. Asking the
	// model to verify first was not enough, so the builder calls the url
	// itself -- through the same client a run uses -- and refuses one a run
	// would fail on.
	probeNote := ""
	if u := httpNodeURLChange(graph, c.name, c.args); u != "" {
		probe, seen := probed[u]
		if !seen {
			probe = fetchURL(ctx, u)
			probed[u] = probe
		}
		refuse, note := judgeProbe(u, probe)
		if refuse != "" {
			return map[string]any{"result": "error: " + refuse}
		}
		probeNote = note
	}
	result, err := applyGraphOp(graph, c.name, c.args)
	if err != nil {
		return map[string]any{"result": "error: " + err.Error()}
	}
	return map[string]any{"result": result + probeNote}
}

// runningLabel says what a call is about to do.
func runningLabel(graph *models.WorkflowGraph, name string, args map[string]any) string {
	switch name {
	case "web_search":
		return fmt.Sprintf("Searching the web for “%s”", clip(argString(args, "query"), 80))
	case "fetch_url":
		return "Checking " + shortURL(argString(args, "url"))
	case "search_x402":
		return fmt.Sprintf("Searching the x402 Bazaar for “%s”", clip(argString(args, "query"), 60))
	case "add_x402_node":
		return "Adding an x402 endpoint"
	case "describe_node":
		return fmt.Sprintf("Reading the %s settings", templateTitle(argString(args, "type"), argString(args, "template")))
	case "add_node":
		label := "Adding " + namedTemplate(argString(args, "type"), argString(args, "template"), argString(args, "name"))
		if httpNodeURLChange(graph, name, args) != "" {
			label += " and checking its URL"
		}
		return label
	case "update_node":
		return fmt.Sprintf("Updating “%s”", nodeName(graph, argString(args, "id")))
	case "remove_node":
		return fmt.Sprintf("Removing “%s”", nodeName(graph, argString(args, "id")))
	case "add_edge":
		return fmt.Sprintf("Connecting “%s” → “%s”", nodeName(graph, argString(args, "from")), nodeName(graph, argString(args, "to")))
	case "remove_edge":
		return "Removing a connection"
	case "test_run":
		return "Test-running the workflow"
	}
	return "Working"
}

// finishedStep describes a call once its response is known. graph is the
// graph AFTER the call, so a node it just added can be named.
func finishedStep(graph *models.WorkflowGraph, name string, args map[string]any, response map[string]any) BuildStep {
	text, _ := response["result"].(string)
	errText, _ := response["error"].(string)
	if strings.HasPrefix(text, "error: ") {
		errText = strings.TrimPrefix(text, "error: ")
	}
	failed := errText != ""
	step := BuildStep{Status: "done"}
	if failed {
		step.Status = "error"
		step.Detail = clip(firstSentence(errText), 180)
	}

	switch name {
	case "web_search":
		step.Kind = "search"
		step.Label = fmt.Sprintf("Searched the web for “%s”", clip(argString(args, "query"), 80))
		if failed {
			step.Label = fmt.Sprintf("Web search for “%s” failed", clip(argString(args, "query"), 80))
			// Never the error text: it can carry a raw upstream body.
			step.Detail = "the search service returned an error"
		}
	case "fetch_url":
		step.Kind = "check"
		var p struct {
			Status int    `json:"status"`
			Error  string `json:"error"`
		}
		_ = json.Unmarshal([]byte(text), &p)
		u := shortURL(argString(args, "url"))
		switch {
		case p.Error != "":
			step.Status, step.Label, step.Detail = "error", "Couldn't reach "+u, clip(firstSentence(p.Error), 180)
		case p.Status >= 200 && p.Status < 300:
			step.Label = fmt.Sprintf("Checked %s → HTTP %d", u, p.Status)
		default:
			step.Status, step.Label = "error", fmt.Sprintf("Checked %s → HTTP %d", u, p.Status)
		}
	case "search_x402":
		step.Kind = "x402"
		q := clip(argString(args, "query"), 60)
		step.Label = fmt.Sprintf("Searched the x402 Bazaar for “%s”", q)
		var r struct {
			Results []any `json:"results"`
		}
		if !failed && json.Unmarshal([]byte(text), &r) == nil {
			step.Label += fmt.Sprintf(" → %d found", len(r.Results))
		}
	case "add_x402_node":
		step.Kind = "x402"
		if failed {
			step.Label = "Couldn't add the x402 endpoint"
		} else if n := lastNode(graph); n != nil {
			step.Label = fmt.Sprintf("Added x402 endpoint “%s”", n.Name)
			if i := strings.Index(text, "costs "); i >= 0 {
				step.Detail = clip(firstSentence(text[i:]), 120)
			}
		}
	case "describe_node":
		step.Kind = "look"
		step.Label = fmt.Sprintf("Read the %s settings", templateTitle(argString(args, "type"), argString(args, "template")))
	case "add_node":
		step.Kind = "node"
		what := namedTemplate(argString(args, "type"), argString(args, "template"), argString(args, "name"))
		if failed {
			step.Label = "Couldn't add " + what
		} else {
			step.Label = "Added " + what
		}
	case "update_node":
		step.Kind = "node"
		n := nodeName(graph, argString(args, "id"))
		step.Label = fmt.Sprintf("Updated “%s”", n)
		if failed {
			step.Label = fmt.Sprintf("Couldn't update “%s”", n)
		}
	case "remove_node":
		step.Kind = "node"
		step.Label = "Removed a step"
		if failed {
			step.Label = "Couldn't remove a step"
		}
	case "add_edge":
		step.Kind = "edge"
		from, to := nodeName(graph, argString(args, "from")), nodeName(graph, argString(args, "to"))
		switch {
		case failed:
			step.Label = fmt.Sprintf("Couldn't connect “%s” → “%s”", from, to)
		case argString(args, "kind") == "attach":
			step.Label = fmt.Sprintf("Attached “%s” to “%s”", from, to)
		default:
			step.Label = fmt.Sprintf("Connected “%s” → “%s”", from, to)
		}
	case "remove_edge":
		step.Kind = "edge"
		step.Label = "Removed a connection"
		if failed {
			step.Label = "Couldn't remove a connection"
		}
	case "test_run":
		step.Kind = "check"
		var r DryRunResult
		body := text
		if i := strings.Index(body, " -- Steps marked simulated"); i >= 0 {
			body = body[:i]
		}
		if failed || json.Unmarshal([]byte(body), &r) != nil {
			step.Label = "Couldn't test-run the workflow"
			break
		}
		switch {
		case r.Failed:
			step.Status = "error"
			step.Label = "Test run failed"
			for _, s := range r.Steps {
				if s.Status == "failed" {
					step.Label = fmt.Sprintf("Test run failed at “%s”", s.Name)
					step.Detail = clip(firstSentence(s.Error), 180)
				}
			}
			if step.Detail == "" {
				step.Detail = clip(firstSentence(r.Error), 180)
			}
		case r.Empty:
			step.Status = "error"
			step.Label = "Test run: a step returned nothing"
			for _, s := range r.Steps {
				if s.Status == "empty" {
					step.Label = fmt.Sprintf("Test run: “%s” returned nothing", s.Name)
					break
				}
			}
		case r.Unverified:
			step.Label = "Test run: part of the workflow can't be checked in a test"
			if names := unverifiedSteps(r); names != "" {
				step.Detail = clip(names, 180)
			}
		default:
			answer := r.Answer
			if answer == "" {
				answer = r.FinalOutput
			}
			step.Label = fmt.Sprintf("Test-ran the workflow → “%s”", clip(answer, 110))
		}
	default:
		step.Kind = "look"
		step.Label = name
	}
	return step
}

func lastNode(graph *models.WorkflowGraph) *models.WorkflowNode {
	if len(graph.Nodes) == 0 {
		return nil
	}
	return &graph.Nodes[len(graph.Nodes)-1]
}

// nodeName is how a node is shown to the user: its own name, else its
// template's catalog name, else its id.
func nodeName(graph *models.WorkflowGraph, id string) string {
	n, ok := findGraphNode(graph, id)
	if !ok {
		return id
	}
	if n.Name != "" {
		return n.Name
	}
	return templateTitle(string(n.Type), n.Template)
}

// templateTitle is a template's catalog display name ("HTTP Request").
func templateTitle(nodeType, template string) string {
	if nodeType == "tool402" {
		return "x402 endpoint"
	}
	if t, ok := catalogTemplate(nodeType, template); ok {
		return t.Name
	}
	if template == "" {
		return nodeType
	}
	return nodeType + "/" + template
}

// namedTemplate is `HTTP Request “Fetch Nifty”`, or just the template title.
func namedTemplate(nodeType, template, name string) string {
	title := templateTitle(nodeType, template)
	if name == "" {
		return title
	}
	return fmt.Sprintf("%s “%s”", title, name)
}

func shortURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return clip(raw, 60)
	}
	return clip(u.Host+u.Path, 60)
}

// firstSentence keeps an error readable in one line: the model-facing
// guidance after the first sentence is for the model, not the user.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{". ", " -- "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSuffix(s, ".")
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
