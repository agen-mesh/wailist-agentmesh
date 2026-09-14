//go:build livebuild

package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestLiveBuildMyradPrice(t *testing.T) {
	key := os.Getenv("PLATFORM_GEMINI_API_KEY")
	if key == "" {
		t.Skip("no platform key")
	}
	msg := os.Getenv("BUILD_MESSAGE")
	if msg == "" {
		msg = "make a workflow which will fetch the price of myrad coin"
	}
	res, err := nodes.BuildGraph(context.Background(), nodes.BuildRequest{
		APIKey:     key,
		Message:    msg,
		Graph:      models.WorkflowGraph{},
		TimeBudget: 110 * time.Second,
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) nodes.DryRunResult {
			return engine.DryRun(ctx, g, engine.DryRunOptions{
				Input:        input,
				PlatformKeys: map[string]string{"gemini": key},
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range res.Graph.Nodes {
		t.Logf("NODE %s %s/%s name=%q cfg=%v prompt=%.80q", n.ID, n.Type, n.Template, n.Name, n.Config, n.SystemPrompt)
	}
	for _, e := range res.Graph.Edges {
		t.Logf("EDGE %s -%s-> %s port=%s", e.From, e.Kind, e.To, e.ToPort)
	}
	t.Logf("REPLY: %s", res.Reply)

	dr := engine.DryRun(context.Background(), res.Graph, engine.DryRunOptions{PlatformKeys: map[string]string{"gemini": key}})
	b, _ := json.MarshalIndent(dr, "", "  ")
	t.Logf("FINAL TEST RUN: %s", b)
}
