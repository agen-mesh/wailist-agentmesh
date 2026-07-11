package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestNotionAction_AppendsParagraphBlock(t *testing.T) {
	var gotPath, gotMethod, gotVersion string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotVersion = r.Header.Get("Notion-Version")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetNotionAPIBaseForTest(srv.URL)
	defer nodes.SetNotionAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "no1", Type: models.NodeTypeAction, Template: "notion",
		Secrets: map[string]string{"notionAPIKey": "secret_xxx"},
		Config:  map[string]string{"notionPageID": "abc123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"daily summary"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "notion_block_appended" {
		t.Errorf("want 'notion_block_appended', got %v", result)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("want PATCH, got %s", gotMethod)
	}
	if gotPath != "/v1/blocks/abc123/children" {
		t.Errorf("want block children path, got %q", gotPath)
	}
	if gotVersion == "" {
		t.Error("want Notion-Version header set")
	}
	if gotBody["children"] == nil {
		t.Errorf("want children in payload, got %v", gotBody)
	}
}

func TestNotionAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "no2", Type: models.NodeTypeAction, Template: "notion",
		Config: map[string]string{"notionPageID": "abc123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"daily summary"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "notion_skipped_no_api_key" {
		t.Errorf("want 'notion_skipped_no_api_key', got %v", result)
	}
}

func TestNotionAction_SkipsWhenNoPageID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "no3", Type: models.NodeTypeAction, Template: "notion",
		Secrets: map[string]string{"notionAPIKey": "secret_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"daily summary"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "notion_skipped_no_page_id" {
		t.Errorf("want 'notion_skipped_no_page_id', got %v", result)
	}
}

func TestAirtableAction_CreatesRecord(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetAirtableAPIBaseForTest(srv.URL)
	defer nodes.SetAirtableAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "at1", Type: models.NodeTypeAction, Template: "airtable",
		Secrets: map[string]string{"airtableAPIKey": "pat_xxx"},
		Config:  map[string]string{"airtableBaseID": "appXXX", "airtableTable": "Tasks"},
	}
	rc := engine.NewRunContext("r1", []byte(`"new lead captured"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "airtable_record_created" {
		t.Errorf("want 'airtable_record_created', got %v", result)
	}
	if gotPath != "/v0/appXXX/Tasks" {
		t.Errorf("want base/table path, got %q", gotPath)
	}
	if gotAuth != "Bearer pat_xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if fields["Notes"] != "new lead captured" {
		t.Errorf("want default field 'Notes' with message, got %v", gotBody)
	}
}

func TestAirtableAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "at2", Type: models.NodeTypeAction, Template: "airtable",
		Config: map[string]string{"airtableBaseID": "appXXX", "airtableTable": "Tasks"},
	}
	rc := engine.NewRunContext("r1", []byte(`"new lead captured"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "airtable_skipped_no_api_key" {
		t.Errorf("want 'airtable_skipped_no_api_key', got %v", result)
	}
}

func TestAirtableAction_SkipsWhenMissingConfig(t *testing.T) {
	node := models.WorkflowNode{
		ID: "at3", Type: models.NodeTypeAction, Template: "airtable",
		Secrets: map[string]string{"airtableAPIKey": "pat_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"new lead captured"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "airtable_skipped_missing_config" {
		t.Errorf("want 'airtable_skipped_missing_config', got %v", result)
	}
}
