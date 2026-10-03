package handlers_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
)

// The allowlist in share_sanitize.go is only as good as the thing that
// notices when somebody adds a field to models.WorkflowNode and forgets it.
// These three sets ARE that thing: every field must appear in exactly one,
// and the first test fails until a new one is classified.
//
// Which set a field belongs in is a decision a person makes. Making it is the
// point -- the failure says "you added a field to a struct that crosses
// accounts; say whether it may travel", which is the question that was never
// asked when maskNodes silently stopped covering the struct it masks.

// carriedNodeFields must come through a share unchanged.
var carriedNodeFields = []string{
	"ID", "Type", "Template", "X", "Y",
	"Name", "Label", "Icon", "Description", "SystemPrompt",
	"Model", "KeyMode",
	"URL", "Method", "Endpoint", "Price", "Unit", "Provider", "Source",
	"EmailSubject", "EmailBody", "EmailProvider",
	"DiscoveredParams", "ParamDefaults", "BodyMode", "BodyTemplate",
	"StateOp", "StateKey", "StateValue",
	"TendrilAction", "TendrilHours", "TendrilAmount",
	"TendrilMinBalance", "TendrilCoverHours",
	"MaxRetries", "RetryBackoffMs",
}

// transformedNodeFields survive in part. Each has its own test below, because
// "partly" is exactly the kind of claim a blanket assertion cannot check.
var transformedNodeFields = []string{"CustomParams", "Config"}

// droppedNodeFields must be absent from a share entirely.
var droppedNodeFields = []string{
	"APIKey", "EmailAPIKey", "Secrets",
	"Wallet", "Balance",
	"EmailTo", "EmailFrom",
	"TendrilNodeID", "TendrilLeaseToken",
}

func TestEveryWorkflowNodeFieldIsClassifiedForSharing(t *testing.T) {
	classified := map[string]string{}
	for _, f := range carriedNodeFields {
		classified[f] = "carried"
	}
	for _, f := range transformedNodeFields {
		if prev, dup := classified[f]; dup {
			t.Fatalf("field %s is listed as both %s and transformed", f, prev)
		}
		classified[f] = "transformed"
	}
	for _, f := range droppedNodeFields {
		if prev, dup := classified[f]; dup {
			t.Fatalf("field %s is listed as both %s and dropped", f, prev)
		}
		classified[f] = "dropped"
	}

	typ := reflect.TypeOf(models.WorkflowNode{})
	var unclassified []string
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if classified[name] == "" {
			unclassified = append(unclassified, name)
		}
		delete(classified, name)
	}
	if len(unclassified) > 0 {
		t.Errorf("models.WorkflowNode has fields nobody has decided about: %s\n"+
			"Add each to carriedNodeFields, transformedNodeFields or droppedNodeFields "+
			"in this file, and to sanitizeNodeForShare if it may travel.",
			strings.Join(unclassified, ", "))
	}
	for name := range classified {
		t.Errorf("field %q is classified here but no longer exists on models.WorkflowNode", name)
	}
}

// fillNode sets every field of a WorkflowNode to a recognisable non-zero
// value, so "was this carried" and "was this dropped" are both answerable
// from the output alone. Reflection rather than a literal, on purpose: a
// literal would silently leave a newly added field at its zero value, and the
// dropped-field assertions would then pass for the wrong reason.
func fillNode(t *testing.T) models.WorkflowNode {
	t.Helper()
	var n models.WorkflowNode
	v := reflect.ValueOf(&n).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := v.Field(i)
		name := typ.Field(i).Name
		switch f.Kind() {
		case reflect.String:
			f.SetString("filled-" + name)
		case reflect.Float64:
			f.SetFloat(float64(i) + 1.5)
		case reflect.Int:
			// Within models.MaxNodeRetries/MaxNodeRetryBackoffMs, so the
			// value is a legal one -- the sanitiser does not clamp, and a
			// test asserting an illegal value round-trips would be asserting
			// the wrong thing.
			f.SetInt(1)
		case reflect.Map:
			f.Set(reflect.ValueOf(map[string]string{"k-" + name: "v-" + name}))
		case reflect.Slice:
			// DiscoveredParams and CustomParams; both are given real contents
			// below, where the test needs specific ones.
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		default:
			t.Fatalf("fillNode does not know how to fill %s (%s) -- teach it", name, f.Kind())
		}
	}
	n.Type = models.NodeTypeAgent
	n.DiscoveredParams = []models.ParamDef{{Name: "city", Type: "string", Required: true}}
	return n
}

func TestSanitizeForShareCarriesPortableFieldsAndDropsTheRest(t *testing.T) {
	in := fillNode(t)
	graph, _ := handlers.SanitizeGraphForShare(models.WorkflowGraph{Nodes: []models.WorkflowNode{in}})
	if len(graph.Nodes) != 1 {
		t.Fatalf("expected 1 node back, got %d", len(graph.Nodes))
	}
	out := graph.Nodes[0]

	inV, outV := reflect.ValueOf(in), reflect.ValueOf(out)
	for _, name := range carriedNodeFields {
		got, want := outV.FieldByName(name).Interface(), inV.FieldByName(name).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("field %s should survive a share unchanged: got %v, want %v", name, got, want)
		}
	}
	for _, name := range droppedNodeFields {
		f := outV.FieldByName(name)
		if !f.IsZero() {
			t.Errorf("field %s must not cross to somebody else, but the share carried %v", name, f.Interface())
		}
	}
}

func TestSanitizeForShareDropsSecretsWholesaleIncludingCiphertext(t *testing.T) {
	// "enc:" is what the at-rest ciphertext looks like, and EncSentinel is
	// the mask the client normally sees. Neither may travel: the first
	// decrypts with the server key on the recipient's next run, and the
	// second lands as a phantom "a key is configured" on a node that has
	// none.
	n := models.WorkflowNode{
		ID:          "n1",
		Type:        models.NodeTypeAgent,
		APIKey:      "enc:0123456789abcdef",
		EmailAPIKey: handlers.EncSentinel,
		Secrets: map[string]string{
			"slackOAuthAccessToken": "enc:deadbeef",
			"webhookSecret":         "a1b2c3d4e5f6",
		},
	}
	graph, red := handlers.SanitizeGraphForShare(models.WorkflowGraph{Nodes: []models.WorkflowNode{n}})
	out := graph.Nodes[0]

	if out.APIKey != "" || out.EmailAPIKey != "" || out.Secrets != nil {
		t.Fatalf("credentials survived sanitising: apiKey=%q emailApiKey=%q secrets=%v",
			out.APIKey, out.EmailAPIKey, out.Secrets)
	}
	if red.APIKeys != 2 {
		t.Errorf("expected 2 API keys counted, got %d", red.APIKeys)
	}
	if red.Secrets != 1 {
		t.Errorf("expected 1 connector secret counted, got %d", red.Secrets)
	}
	// Counted apart: the recipient gets a fresh one minted on save, so this
	// is not something they have to go and supply.
	if red.WebhookSecrets != 1 {
		t.Errorf("expected the webhook secret counted separately, got %d", red.WebhookSecrets)
	}
}

func TestSanitizeForShareKeepsAFileParamsShapeButNotItsBytes(t *testing.T) {
	n := models.WorkflowNode{
		ID:   "n1",
		Type: models.NodeTypeTool402,
		CustomParams: []models.CustomParam{
			{Name: "task", Kind: "text", Value: "screen this resume"},
			{Name: "resume", Kind: "file", Value: "JVBERi0xLjQK", FileName: "cv.pdf", MIMEType: "application/pdf"},
		},
	}
	graph, red := handlers.SanitizeGraphForShare(models.WorkflowGraph{Nodes: []models.WorkflowNode{n}})
	got := graph.Nodes[0].CustomParams

	if len(got) != 2 {
		t.Fatalf("expected both params kept, got %d", len(got))
	}
	if got[0].Value != "screen this resume" {
		t.Errorf("a text param is configuration and should survive, got %q", got[0].Value)
	}
	if got[1].Value != "" {
		t.Errorf("the uploaded file's bytes must not travel, got %q", got[1].Value)
	}
	if got[1].FileName != "cv.pdf" || got[1].MIMEType != "application/pdf" {
		t.Errorf("the recipient still needs to know a file goes here, and which kind: %+v", got[1])
	}
	if red.UploadedFiles != 1 {
		t.Errorf("expected 1 uploaded file counted, got %d", red.UploadedFiles)
	}
}

func TestSanitizeForShareDropsAccountBindingConfigButKeepsRouting(t *testing.T) {
	n := models.WorkflowNode{
		ID:   "n1",
		Type: models.NodeTypeAction,
		Config: map[string]string{
			// Bound to the sharer's account -- must go.
			"oauthCredentialID":     "cred_0000",
			"slackOAuthAccessToken": "xoxb-fixture",
			"jiraOAuthCloudID":      "cloud-fixture",
			"mailchimpOAuthDC":      "us1",
			"jiraEmail":             "someone@example.invalid",
			"twilioTo":              "+10000000000",
			// Describes what the workflow does -- must stay.
			"slackChannel": "#releases",
			"githubRepo":   "acme/widgets",
		},
	}
	graph, red := handlers.SanitizeGraphForShare(models.WorkflowGraph{Nodes: []models.WorkflowNode{n}})
	got := graph.Nodes[0].Config

	for _, k := range []string{"oauthCredentialID", "slackOAuthAccessToken", "jiraOAuthCloudID", "mailchimpOAuthDC", "jiraEmail", "twilioTo"} {
		if _, present := got[k]; present {
			t.Errorf("config %q binds the node to the sharer's account and must not travel", k)
		}
	}
	if got["slackChannel"] != "#releases" || got["githubRepo"] != "acme/widgets" {
		t.Errorf("routing config is what the workflow does and should survive, got %v", got)
	}
	if red.ConnectedAccounts != 6 {
		t.Errorf("expected 6 account-bound settings counted, got %d", red.ConnectedAccounts)
	}
}

func TestSanitizeForShareRejectsAGraphThatCannotBeWired(t *testing.T) {
	// Neither shape can come out of our own canvas. Both can come out of a
	// pasted code, and a dangling edge reaches the topological sort as a
	// reference to a node that is not there.
	graph, _ := handlers.SanitizeGraphForShare(models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n1", Type: models.NodeTypeAgent, Name: "impostor"},
			{ID: "", Type: models.NodeTypeAgent},
			{ID: "n2", Type: models.NodeTypeAgent},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n1", To: "ghost", Kind: models.EdgeKindFlow},
			{ID: "e3", From: "ghost", To: "n2", Kind: models.EdgeKindFlow},
		},
	})

	if len(graph.Nodes) != 2 {
		t.Fatalf("expected the duplicate and the id-less node dropped, got %d nodes", len(graph.Nodes))
	}
	if graph.Nodes[0].Name == "impostor" {
		t.Error("the first node with an id should win, not a later one reusing it")
	}
	if len(graph.Edges) != 1 || graph.Edges[0].ID != "e1" {
		t.Fatalf("expected only the edge between two surviving nodes, got %+v", graph.Edges)
	}
}

// A recipient importing a connector workflow is told to reconnect it.
//
// The connector's token lives in node.Secrets under
// connectorSecretKey(provider), which the sanitiser drops wholesale -- so by
// the time requirements are worked out there is no trace of what was taken.
// The surviving signal is the node's Template, which for an action node IS
// the provider name. Before this, the preview told somebody importing a Slack
// workflow that it was "ready to run as-is", and it was not.
func TestRequirementsNameTheConnectorsTheRecipientMustReconnect(t *testing.T) {
	var d handlers.Deps

	graph, _ := handlers.SanitizeGraphForShare(models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger, Template: "webhook"},
			{
				ID: "n2", Type: models.NodeTypeAction, Template: "slack",
				Secrets: map[string]string{"slackOAuthAccessToken": "xoxb-fixture"},
			},
			// A second node on the same connector is still one account.
			{
				ID: "n3", Type: models.NodeTypeAction, Template: "slack",
				Secrets: map[string]string{"slackOAuthAccessToken": "xoxb-fixture"},
			},
			{
				ID: "n4", Type: models.NodeTypeAction, Template: "github",
				Secrets: map[string]string{"githubOAuthAccessToken": "gho-fixture"},
			},
			// Not a connector: no account to reconnect, nothing to say.
			{ID: "n5", Type: models.NodeTypeAction, Template: "email"},
		},
	})

	req := d.RequirementsForImport(graph)
	want := []string{"github", "slack"} // sorted, so the sentence is stable
	if len(req.Connectors) != len(want) {
		t.Fatalf("Connectors = %v, want %v", req.Connectors, want)
	}
	for i, name := range want {
		if req.Connectors[i] != name {
			t.Fatalf("Connectors = %v, want %v", req.Connectors, want)
		}
	}
	if !req.Any() {
		t.Error("a graph needing two accounts reconnected reports nothing to do")
	}
}

func TestSanitizeForShareDropsAnEdgeThatReusesAnotherEdgesID(t *testing.T) {
	// The canvas keys its rendered edges by id, so two edges sharing one give
	// the recipient a key collision: one of the pair does not draw, and the
	// imported graph reads as wired differently from the one that was shared.
	// Our own canvas cannot produce this; a pasted code can.
	graph, _ := handlers.SanitizeGraphForShare(models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeAgent},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e1", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
			// No id at all is left alone: the canvas has always tolerated
			// those, and minting a key for one is not the sanitiser's job.
			{From: "n1", To: "n3", Kind: models.EdgeKindFlow},
		},
	})

	if len(graph.Edges) != 2 {
		t.Fatalf("expected the duplicate id dropped and the id-less edge kept, got %+v", graph.Edges)
	}
	if graph.Edges[0].To != "n2" {
		t.Error("the first edge with an id should win, not a later one reusing it")
	}
	if graph.Edges[1].ID != "" || graph.Edges[1].To != "n3" {
		t.Errorf("the id-less edge should have survived, got %+v", graph.Edges[1])
	}
}

func TestSanitizeForShareReportsNothingRemovedForACleanGraph(t *testing.T) {
	graph, red := handlers.SanitizeGraphForShare(models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeTrigger, Template: "manual"}},
	})
	if red.Any() {
		t.Errorf("a graph with nothing sensitive in it should report no redactions, got %+v", red)
	}
	if len(graph.Nodes) != 1 {
		t.Errorf("expected the node kept, got %d", len(graph.Nodes))
	}
	// Non-nil empty slices, so a share serialises as [] rather than null.
	if graph.Edges == nil {
		t.Error("edges should serialise as [] on a graph with no edges, not null")
	}
}
