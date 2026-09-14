//go:build livebuild

package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

const bazaarBase = "https://facilitator.goplausible.xyz"

func liveCatalog(t *testing.T) func(context.Context) ([]bazaar.Resource, error) {
	t.Helper()
	var cached []bazaar.Resource
	return func(ctx context.Context) ([]bazaar.Resource, error) {
		if cached != nil {
			return cached, nil
		}
		fetched, err := bazaar.FetchAll(ctx, &http.Client{Timeout: 60 * time.Second}, bazaarBase)
		if err != nil {
			return nil, err
		}
		cached = bazaar.Merge(fetched)
		return cached, nil
	}
}

// Builds that must reach for a paid x402 endpoint from the Bazaar, with the
// real catalog behind search_x402/add_x402_node.
func TestLiveBuildX402(t *testing.T) {
	key := os.Getenv("PLATFORM_GEMINI_API_KEY")
	if key == "" {
		t.Skip("no platform key")
	}
	// The websearch tool node reads the package-level key the server sets at
	// boot, not DryRunOptions -- without this it fails every test run with
	// "platform Gemini key is not configured".
	nodes.SetPlatformKeys(map[string]string{"gemini": key})
	catalog := liveCatalog(t)
	if items, err := catalog(context.Background()); err != nil {
		t.Fatalf("bazaar unreachable: %v", err)
	} else {
		t.Logf("bazaar catalog: %d resources", len(items))
	}
	prompts := []string{
		"use a paid x402 endpoint from the bazaar to get me a crypto price, and have an agent explain it",
		"build a workflow that pays an x402 endpoint for live market data and summarises what it returns",
		"I want an agent that can call a paid x402 tool from the bazaar when it needs live data, over chat",
	}
	for _, p := range prompts {
		t.Run(p[:28], func(t *testing.T) {
			res, err := nodes.BuildGraph(context.Background(), nodes.BuildRequest{
				APIKey: key, Message: p, Graph: models.WorkflowGraph{},
				TimeBudget: 110 * time.Second, X402Catalog: catalog,
				TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) nodes.DryRunResult {
					return engine.DryRun(ctx, g, engine.DryRunOptions{Input: input, PlatformKeys: map[string]string{"gemini": key}})
				},
			})
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			t.Logf("PROMPT: %s", p)
			triggers := 0
			for _, n := range res.Graph.Nodes {
				if n.Type == models.NodeTypeTrigger {
					triggers++
				}
				extra := ""
				if n.Type == models.NodeTypeTool402 {
					extra = " endpoint=" + n.Endpoint + " price=" + n.Price + " " + n.Unit
					b, _ := json.Marshal(n.DiscoveredParams)
					extra += " params=" + string(b)
				}
				t.Logf("  NODE %s/%s %q%s", n.Type, n.Template, n.Name, extra)
			}
			for _, e := range res.Graph.Edges {
				t.Logf("  EDGE %s -%s-> %s (%s)", e.From, e.Kind, e.To, e.ToPort)
			}
			t.Logf("  TRIGGERS: %d", triggers)
			dr := engine.DryRun(context.Background(), res.Graph, engine.DryRunOptions{PlatformKeys: map[string]string{"gemini": key}})
			for _, s := range dr.Steps {
				t.Logf("  STEP %-26s %-10s %.90s", s.Name, s.Status, s.Output+s.Error+s.Reason)
			}
			t.Logf("  RESULT failed=%v empty=%v unverified=%v", dr.Failed, dr.Empty, dr.Unverified)
			t.Logf("  ANSWER: %.200s", dr.FinalOutput)
			t.Logf("  REPLY: %.500s", strings.ReplaceAll(res.Reply, "\n", " "))
		})
	}
}
