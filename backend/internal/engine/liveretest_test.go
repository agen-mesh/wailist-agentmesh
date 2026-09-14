//go:build livebuild

package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestLiveBuildRetest(t *testing.T) {
	key := os.Getenv("PLATFORM_GEMINI_API_KEY")
	if key == "" {
		t.Skip("no platform key")
	}
	cases := []struct{ category, prompt string }{
		{"crypto", "check the price of ethereum and save the latest value into workflow state"},
		{"news", "fetch the top 10 hacker news stories and email them to me"},
		{"news", "monitor the RSS feed https://hnrss.org/frontpage and post new items to telegram"},
		{"weather", "ask me which city I want and then tell me the weather there"},
		{"http", "check if https://example.com is up and tell me the HTTP status"},
		{"fan-in", "give me a daily digest with both the top hacker news story and the bitcoin price in one message"},
	}
	out := make([]promptResult, 0, len(cases))
	for _, c := range cases {
		r := promptResult{Prompt: c.prompt, Category: c.category}
		started := time.Now()
		res, err := nodes.BuildGraph(context.Background(), nodes.BuildRequest{
			APIKey: key, Message: c.prompt, Graph: models.WorkflowGraph{}, TimeBudget: 110 * time.Second,
			TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) nodes.DryRunResult {
				return engine.DryRun(ctx, g, engine.DryRunOptions{Input: input, PlatformKeys: map[string]string{"gemini": key}})
			},
		})
		r.BuildSecs = time.Since(started).Seconds()
		if err != nil {
			r.BuildErr = err.Error()
			out = append(out, r)
			t.Logf("DONE %s (build error: %v)", c.prompt, err)
			continue
		}
		r.Reply, r.Nodes, r.Edges = res.Reply, len(res.Graph.Nodes), len(res.Graph.Edges)
		seen := map[string]bool{}
		for _, e := range res.Graph.Edges {
			k := e.From + string(e.Kind) + e.To + e.ToPort
			if seen[k] {
				r.DupEdges++
			}
			seen[k] = true
			if e.Kind == models.EdgeKindAttach && e.ToPort == "model" {
				r.HasModel = true
			}
		}
		for _, n := range res.Graph.Nodes {
			r.Chain = append(r.Chain, string(n.Type)+"/"+n.Template)
			if n.Type == models.NodeTypeAgent {
				r.HasAgent = true
			}
		}
		dr := engine.DryRun(context.Background(), res.Graph, engine.DryRunOptions{PlatformKeys: map[string]string{"gemini": key}})
		r.Failed, r.Empty, r.Unverified = dr.Failed, dr.Empty, dr.Unverified
		for _, s := range dr.Steps {
			switch s.Status {
			case "ran":
				r.StepsRan++
			case "simulated":
				r.Simulated++
			}
			if s.Reason != "" {
				r.Reasons = append(r.Reasons, s.Name+": "+s.Reason)
			}
			if s.Error != "" {
				r.Reasons = append(r.Reasons, s.Name+" ERROR: "+s.Error)
			}
		}
		answer := dr.FinalOutput
		if dr.FinalSimulated && dr.WouldSend != "" {
			answer = dr.WouldSend
		}
		r.Answer = answer
		a := strings.TrimSpace(answer)
		r.RawAnswer = strings.HasPrefix(a, "{") || strings.HasPrefix(a, "[")
		out = append(out, r)
		t.Logf("DONE [%s] %s -> failed=%v empty=%v unverified=%v", c.category, c.prompt, r.Failed, r.Empty, r.Unverified)
		b, _ := json.MarshalIndent(out, "", "  ")
		os.WriteFile(os.Getenv("REPORT_OUT"), b, 0o644)
	}
}
