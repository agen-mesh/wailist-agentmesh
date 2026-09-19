package nodes

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// Parallel branches that read each other's data.
//
// A step that takes "its input" does not get its predecessor's output. It
// gets RunContext.Message(): the most recent output anywhere in the run
// (engine/context.go). The engine runs every step in a topological level at
// once, so when two steps share a level -- or a step's predecessor shares
// one -- which output is "most recent" is decided by goroutine scheduling.
// That rule is load-bearing for payments and is not changed here.
//
// A live build for "crypto news and the ALGO price every morning" fanned out
// into two chains, trigger -> RSS -> news agent and trigger -> price -> price
// agent. Both agents read whichever fetch finished last, and the answer the
// user saw mentioned only the price. Every edge in that graph was legal and
// the test run "passed".
//
// So the audit flags a step that reads implicitly while another step can
// write at the same moment, and says how to build it instead: fetch in
// parallel if you like, but combine the results in an Edit Fields step that
// names each source with {{ node.<id> }}, and run one chain from there.

// nodeRefIDPattern matches {{ node.<id> }} and {{ node.<id>.field }}. The same
// shape as engine.nodeRefPattern, which this package cannot import.
var nodeRefIDPattern = regexp.MustCompile(`\{\{\s*node\.([A-Za-z0-9_\-]+)(?:\.[A-Za-z0-9_.\-]+)?\s*\}\}`)

// templateStrings mirrors engine.templateEligibleStrings: the fields a
// connector may run through resolveTemplate.
func templateStrings(n models.WorkflowNode) []string {
	out := []string{n.EmailBody}
	for _, v := range n.Config {
		out = append(out, v)
	}
	for _, v := range n.ParamDefaults {
		out = append(out, v)
	}
	for _, p := range n.CustomParams {
		out = append(out, p.Value)
	}
	return out
}

func nodeRefIDs(n models.WorkflowNode) map[string]bool {
	ids := map[string]bool{}
	for _, s := range templateStrings(n) {
		for _, m := range nodeRefIDPattern.FindAllStringSubmatch(s, -1) {
			ids[m[1]] = true
		}
	}
	return ids
}

// readsImplicitly reports whether n takes "the latest output" as its input.
//
// A read connector with no {{ }} in its settings fetches what its settings
// name and ignores its input (an RSS feed, a CoinGecko price), so it cannot
// pick up the wrong one. A step that names every one of its flow
// predecessors by id reads exactly those. Everything else -- agents, end
// nodes, parsers, senders, paid tools -- reads the latest output.
func readsImplicitly(n models.WorkflowNode, preds []string) bool {
	if n.Type == models.NodeTypeTrigger || n.Type == models.NodeTypeProvider {
		return false
	}
	refs := nodeRefIDs(n)
	if len(preds) > 0 && len(refs) > 0 {
		all := true
		for _, p := range preds {
			if !refs[p] {
				all = false
				break
			}
		}
		if all {
			return false
		}
	}
	if tpl, ok := catalogTemplate(string(n.Type), n.Template); ok && tpl.Kind == "read" {
		for _, s := range templateStrings(n) {
			if strings.Contains(s, "{{") {
				return true
			}
		}
		return false
	}
	return true
}

// flowLevels assigns each node the topological level the engine would run it
// in: flow edges plus {{ node.<id> }} references, over every node (as
// engine.TopologicalSort does). Nil when the flow loops.
func flowLevels(graph models.WorkflowGraph) map[string]int {
	inDeg := map[string]int{}
	succ := map[string][]string{}
	exists := map[string]bool{}
	for _, n := range graph.Nodes {
		exists[n.ID] = true
		inDeg[n.ID] = 0
	}
	seen := map[[2]string]bool{}
	add := func(from, to string) {
		if from == to || !exists[from] || !exists[to] || seen[[2]string{from, to}] {
			return
		}
		seen[[2]string{from, to}] = true
		succ[from] = append(succ[from], to)
		inDeg[to]++
	}
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindFlow {
			add(e.From, e.To)
		}
	}
	for _, n := range graph.Nodes {
		for id := range nodeRefIDs(n) {
			add(id, n.ID)
		}
	}
	level := map[string]int{}
	var queue []string
	for _, n := range graph.Nodes {
		if inDeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	for depth := 0; len(queue) > 0; depth++ {
		var next []string
		for _, id := range queue {
			level[id] = depth
			for _, s := range succ[id] {
				inDeg[s]--
				if inDeg[s] == 0 {
					next = append(next, s)
				}
			}
		}
		queue = next
	}
	if len(level) != len(graph.Nodes) {
		return nil
	}
	return level
}

// racyInputFindings returns one audit finding per step that can read another
// branch's output.
func racyInputFindings(graph models.WorkflowGraph) []string {
	level := flowLevels(graph)
	if level == nil {
		return nil // the loop finding covers it
	}
	// Attached tools and providers never run as steps, so they never write.
	attached := map[string]bool{}
	preds := map[string][]string{}
	for _, e := range graph.Edges {
		switch e.Kind {
		case models.EdgeKindAttach:
			attached[e.From] = true
		case models.EdgeKindFlow:
			preds[e.To] = append(preds[e.To], e.From)
		}
	}
	writersAt := map[int][]string{}
	for _, n := range graph.Nodes {
		if !attached[n.ID] && n.Type != models.NodeTypeProvider {
			writersAt[level[n.ID]] = append(writersAt[level[n.ID]], n.ID)
		}
	}

	var findings []string
	for _, n := range graph.Nodes {
		if attached[n.ID] || len(preds[n.ID]) == 0 || !readsImplicitly(n, preds[n.ID]) {
			continue
		}
		// Who else can have written the "latest output" when n reads it:
		// its own level-mates, and whatever shares a level with each step
		// feeding it.
		rivals := map[string]bool{}
		for _, id := range writersAt[level[n.ID]] {
			if id != n.ID {
				rivals[id] = true
			}
		}
		for _, p := range preds[n.ID] {
			for _, id := range writersAt[level[p]] {
				if id != p {
					rivals[id] = true
				}
			}
		}
		if len(preds[n.ID]) > 1 {
			for _, p := range preds[n.ID] {
				rivals[p] = true
			}
		}
		if len(rivals) == 0 {
			continue
		}
		names := make([]string, 0, len(rivals))
		for id := range rivals {
			names = append(names, fmt.Sprintf("%q", id))
		}
		sort.Strings(names)
		findings = append(findings, fmt.Sprintf(
			"step %q (%s/%s) takes whichever output was produced last as its input, and %s can produce one at the same time -- so it may read the wrong branch's data and the answer can silently lose a source. "+
				"Do not run separate chains side by side: fetch the sources, flow them all into ONE Edit Fields step (tool/set) whose setFields names each with {{ node.<id> }}, and run a single chain (one agent, then the end) from that step",
			n.ID, n.Type, n.Template, strings.Join(slices.Compact(names), ", ")))
	}
	return findings
}
