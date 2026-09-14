//go:build livebuild

package engine_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestLiveBuildPrompts(t *testing.T) {
	key := os.Getenv("PLATFORM_GEMINI_API_KEY")
	if key == "" {
		t.Skip("no platform key")
	}
	prompts := []string{
		"get the current weather in Kolkata and tell me whether I need an umbrella today",
		"fetch the price of bitcoin and ethereum and tell me which one is worth more right now",
		"summarise the top hacker news stories about AI",
		"every morning email me a summary of the latest posts from the hacker news front page",
		"I want to chat with an agent that can answer questions about anything using live web search",
		"read the RSS feed at https://hnrss.org/frontpage and give me the three most interesting titles",
	}
	for _, p := range prompts {
		t.Run(p[:24], func(t *testing.T) {
			res, err := nodes.BuildGraph(context.Background(), nodes.BuildRequest{
				APIKey: key, Message: p, Graph: models.WorkflowGraph{}, TimeBudget: 110 * time.Second,
				TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) nodes.DryRunResult {
					return engine.DryRun(ctx, g, engine.DryRunOptions{Input: input, PlatformKeys: map[string]string{"gemini": key}})
				},
			})
			if err != nil {
				t.Fatalf("PROMPT %q: build failed: %v", p, err)
			}
			var chain []string
			for _, n := range res.Graph.Nodes {
				chain = append(chain, string(n.Type)+"/"+n.Template)
			}
			t.Logf("PROMPT: %s", p)
			t.Logf("  NODES: %s", strings.Join(chain, ", "))
			for _, e := range res.Graph.Edges {
				t.Logf("  EDGE %s -%s-> %s (%s)", nodeLabel(res.Graph, e.From), e.Kind, nodeLabel(res.Graph, e.To), e.ToPort)
			}
			dr := engine.DryRun(context.Background(), res.Graph, engine.DryRunOptions{PlatformKeys: map[string]string{"gemini": key}})
			for _, s := range dr.Steps {
				t.Logf("  STEP %-28s %-10s %.100s", s.Name, s.Status, s.Output+s.Error+s.Reason)
			}
			t.Logf("  RESULT failed=%v empty=%v unverified=%v answer=%q", dr.Failed, dr.Empty, dr.Unverified, dr.FinalOutput)
			t.Logf("  REPLY: %.400s", res.Reply)
		})
	}
}

func nodeLabel(g models.WorkflowGraph, id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			return string(n.Type) + "/" + n.Template
		}
	}
	return id
}
