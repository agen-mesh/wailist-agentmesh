package nodes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>Example Feed</title>
  <item><title>First post</title><link>https://example.com/1</link><pubDate>Mon, 04 Aug 2026 10:00:00 GMT</pubDate></item>
  <item><title>Second post</title><link>https://example.com/2</link><pubDate>Tue, 05 Aug 2026 10:00:00 GMT</pubDate></item>
  <item><title>Third post</title><link>https://example.com/3</link><pubDate>Wed, 06 Aug 2026 10:00:00 GMT</pubDate></item>
</channel></rss>`

func TestRSSAction_ReturnsItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", nil)

	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want a structured result, got %T", out)
	}
	if got["title"] != "Example Feed" {
		t.Errorf("feed title: got %v", got["title"])
	}
	items, ok := got["items"].([]map[string]any)
	if !ok {
		t.Fatalf("want an items slice, got %T", got["items"])
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if items[0]["title"] != "First post" {
		t.Errorf("first item title: got %v", items[0]["title"])
	}
	if items[0]["link"] != "https://example.com/1" {
		t.Errorf("first item link: got %v", items[0]["link"])
	}
}

func TestRSSAction_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL, "rssLimit": "2"},
	}
	rc := engine.NewRunContext("r1", nil)
	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	items := out.(map[string]any)["items"].([]map[string]any)
	if len(items) != 2 {
		t.Errorf("want 2 items with rssLimit=2, got %d", len(items))
	}
}

func TestRSSAction_SkipsWithoutURL(t *testing.T) {
	node := models.WorkflowNode{ID: "rss1", Type: models.NodeTypeAction, Template: "rss"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "rss_skipped_no_url" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestRSSAction_ErrorsOnMalformedFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a feed at all"))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", nil)
	if _, err := nodes.ExecuteAction(context.Background(), node, rc); err == nil {
		t.Error("want an error for a non-feed response, got nil")
	}
}
