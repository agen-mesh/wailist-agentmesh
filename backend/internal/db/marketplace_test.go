package db_test

import (
	"context"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestPublishedWorkflowCRUD(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, err := store.CreateUser(ctx, "creator@test.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeTrigger}},
		Edges: []models.WorkflowEdge{},
	}

	pw, err := store.PublishWorkflow(ctx, creator.ID, "My Workflow", "Does stuff", []string{"ai", "search"}, graph, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if pw.Title != "My Workflow" {
		t.Fatalf("want title 'My Workflow' got %q", pw.Title)
	}
	if pw.CreatorID != creator.ID {
		t.Fatal("creator id mismatch")
	}

	// List returns it
	list, err := store.ListPublishedWorkflows(ctx, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range list {
		if w.ID == pw.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("published workflow not in list")
	}

	// Search by title
	results, err := store.ListPublishedWorkflows(ctx, "My Workflow", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results")
	}

	// Search no match
	empty, err := store.ListPublishedWorkflows(ctx, "zzznomatch", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatal("expected empty search result")
	}
}

func TestImportPublishedWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator2@test.com", "hash")
	importer, _ := store.CreateUser(ctx, "importer@test.com", "hash")

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeAgent}},
		Edges: []models.WorkflowEdge{},
	}
	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Importable WF", "desc", []string{}, graph, 0.005)

	imported, err := store.ImportPublishedWorkflow(ctx, importer.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if imported.UserID != importer.ID {
		t.Fatal("importer id mismatch")
	}
	if imported.SourcePublishedID == nil || *imported.SourcePublishedID != pw.ID {
		t.Fatal("source_published_id not set on imported workflow")
	}
	if len(imported.Nodes) != 1 {
		t.Fatal("nodes not copied from published workflow")
	}
}

func TestToggleUpvote(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator3@test.com", "hash")
	voter, _ := store.CreateUser(ctx, "voter@test.com", "hash")

	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Votable WF", "desc", []string{}, models.WorkflowGraph{}, 0)

	// First vote: upvote
	count, upvoted, err := store.ToggleUpvote(ctx, voter.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !upvoted {
		t.Fatal("expected upvoted=true on first vote")
	}
	if count != 1 {
		t.Fatalf("expected count=1 got %d", count)
	}

	// Second vote: unvote
	count2, upvoted2, err := store.ToggleUpvote(ctx, voter.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if upvoted2 {
		t.Fatal("expected upvoted=false on second vote (unvote)")
	}
	if count2 != 0 {
		t.Fatalf("expected count=0 got %d", count2)
	}
}

func TestCreditMarketplaceCreator(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator4@test.com", "hash")
	importer, _ := store.CreateUser(ctx, "importer2@test.com", "hash")

	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Paid WF", "desc", []string{}, models.WorkflowGraph{}, 0.01)
	imported, _ := store.ImportPublishedWorkflow(ctx, importer.ID, pw.ID)

	initialCredits := creator.Credits

	err := store.CreditMarketplaceCreator(ctx, imported.ID, 1.0) // 1% of $1.00 = $0.01
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetUserByID(ctx, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := initialCredits + 0.01
	if updated.Credits < want-0.0001 || updated.Credits > want+0.0001 {
		t.Fatalf("expected credits ~%.4f got %.4f", want, updated.Credits)
	}

	// Check run_count incremented on published workflow
	list, _ := store.ListPublishedWorkflows(ctx, "Paid WF", 10, 0)
	if len(list) == 0 {
		t.Fatal("workflow not found")
	}
	if list[0].RunCount != 1 {
		t.Fatalf("expected run_count=1 got %d", list[0].RunCount)
	}
}
