package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
)

// The share round trip, against a real database.
//
// testDeps/withURLParam come from workflows_test.go in this package; both skip
// without TEST_DATABASE_URL, the convention every DB-touching test here
// follows.

// shareFixtureNodes is a workflow with one of everything that must not
// travel: a BYOK key, a connector secret, a webhook secret, an agent wallet,
// real addresses on an email node, a Google connection bound to the sharer's
// own OAuth row, and an uploaded file's bytes.
func shareFixtureNodes() []models.WorkflowNode {
	return []models.WorkflowNode{
		{
			ID: "n_trigger", Type: models.NodeTypeTrigger, Template: "webhook",
			Name: "When called",
		},
		{
			ID: "n_provider", Type: models.NodeTypeProvider, Template: "openai",
			Model: "gpt-fixture", APIKey: "sk-test-fixture-value",
		},
		{
			ID: "n_agent", Type: models.NodeTypeAgent,
			SystemPrompt: "Summarise the input.",
			Wallet:       "FIXTUREWALLETADDRESS7777777777777777777777777777777777777",
			Balance:      "12.5",
		},
		{
			ID: "n_email", Type: models.NodeTypeAction, Template: "email",
			EmailTo: "recipient@example.invalid", EmailFrom: "sender@example.invalid",
			EmailSubject: "Your summary", EmailBody: "{{input}}",
			EmailAPIKey: "re_test_fixture",
			Secrets:     map[string]string{"slackOAuthAccessToken": "xoxb-fixture"},
			Config:      map[string]string{"slackChannel": "#releases", "oauthCredentialID": "cred_fixture"},
		},
		{
			ID: "n_tool", Type: models.NodeTypeTool402, Endpoint: "https://example.invalid/screen",
			CustomParams: []models.CustomParam{
				{Name: "resume", Kind: "file", Value: "JVBERi0xLjQKZml4dHVyZQ==", FileName: "cv.pdf", MIMEType: "application/pdf"},
			},
		},
	}
}

// shareFixtureEdges wires the fixture up: a flow through the agent, and the
// attach edges that give it its model and its tool.
func shareFixtureEdges() []models.WorkflowEdge {
	return []models.WorkflowEdge{
		{ID: "e1", From: "n_trigger", To: "n_agent", Kind: models.EdgeKindFlow},
		{ID: "e2", From: "n_provider", To: "n_agent", Kind: models.EdgeKindAttach, ToPort: "model"},
		{ID: "e3", From: "n_tool", To: "n_agent", Kind: models.EdgeKindAttach, ToPort: "tools"},
		{ID: "e4", From: "n_agent", To: "n_email", Kind: models.EdgeKindFlow},
	}
}

// seedSharedWorkflow creates a workflow owned by userID, saved through the
// real UpdateWorkflow so the stored nodes are encrypted and webhook-secreted
// exactly as a user's own save would leave them. Sharing a hand-written row
// would prove nothing about the path that actually matters.
func seedSharedWorkflow(t *testing.T, d *handlers.Deps, userID string) models.Workflow {
	t.Helper()
	ctx := context.Background()

	wf, err := d.Store.CreateWorkflow(ctx, "Resume screener", userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	saveSharedWorkflow(t, d, wf.ID, userID, shareFixtureEdges())

	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// saveSharedWorkflow writes the fixture nodes and the given edges through the
// real UpdateWorkflow, which is also how a test changes the graph between two
// share attempts.
func saveSharedWorkflow(t *testing.T, d *handlers.Deps, workflowID, userID string, edges []models.WorkflowEdge) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":  "Resume screener",
		"nodes": shareFixtureNodes(),
		"edges": edges,
	})
	req := httptest.NewRequest(http.MethodPut, "/workflows/"+workflowID, bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "id", workflowID)
	w := httptest.NewRecorder()
	d.UpdateWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("saving the workflow failed: %d %s", w.Code, w.Body.String())
	}
}

// postShare drives CreateShare and hands back the status alongside the body,
// for the tests that care which of 200 (reused) and 201 (minted) came back.
func postShare(t *testing.T, d *handlers.Deps, workflowID, userID, body string) (int, models.WorkflowShare, handlers.ShareRedactions) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/workflows/"+workflowID+"/share", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "id", workflowID)
	w := httptest.NewRecorder()
	d.CreateShare(w, req)
	var out struct {
		Share      models.WorkflowShare     `json:"share"`
		Redactions handlers.ShareRedactions `json:"redactions"`
	}
	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, out.Share, out.Redactions
}

// createShare drives the handler and returns the decoded share, failing the
// test on any non-201.
func createShare(t *testing.T, d *handlers.Deps, workflowID, userID, body string) (models.WorkflowShare, handlers.ShareRedactions) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/workflows/"+workflowID+"/share", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "id", workflowID)
	w := httptest.NewRecorder()
	d.CreateShare(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateShare = %d, want 201: %s", w.Code, w.Body.String())
	}
	var out struct {
		Share      models.WorkflowShare     `json:"share"`
		Redactions handlers.ShareRedactions `json:"redactions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Share, out.Redactions
}

func TestSharedSnapshotCarriesNothingThatBelongsToTheSharer(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	share, red := createShare(t, d, wf.ID, owner, `{}`)

	// The bluntest assertion available, and the one worth having: whatever
	// the structure, none of these strings may appear anywhere in what a
	// recipient is served. A field added later that leaks one of them fails
	// here even if nobody thought to assert on it by name.
	raw, _ := json.Marshal(share)
	for _, forbidden := range []string{
		"sk-test-fixture-value", // the BYOK key
		"re_test_fixture",       // the email provider key
		"xoxb-fixture",          // a connector secret
		"enc:",                  // ciphertext of any of the above
		handlers.EncSentinel,    // the mask, which imports as a phantom key
		"webhookSecret",         // the credential for the public trigger
		"FIXTUREWALLETADDRESS",  // the sharer's agent wallet
		"recipient@example.invalid",
		"sender@example.invalid",
		"cred_fixture",             // the sharer's Google connection
		"JVBERi0xLjQKZml4dHVyZQ==", // the uploaded file's bytes
	} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Errorf("the shared snapshot carries %q, which belongs to the sharer", forbidden)
		}
	}

	// And the other half: it is still the workflow, not a husk.
	if share.NodeCount != 5 || share.EdgeCount != 4 {
		t.Errorf("expected the whole graph shared, got %d nodes / %d edges", share.NodeCount, share.EdgeCount)
	}
	if !strings.Contains(string(raw), "Summarise the input.") {
		t.Error("the agent's prompt is what the workflow does and should survive")
	}
	if !strings.Contains(string(raw), "https://example.invalid/screen") {
		t.Error("a tool node without its endpoint is not importable")
	}
	if !strings.Contains(string(raw), "cv.pdf") {
		t.Error("the recipient still needs to know a file goes here")
	}

	if red.APIKeys != 2 || red.Secrets != 1 || red.WebhookSecrets != 1 ||
		red.UploadedFiles != 1 || red.AgentWallets != 1 || red.EmailAddresses != 1 ||
		red.ConnectedAccounts != 1 {
		t.Errorf("redaction counts shown to the sharer look wrong: %+v", red)
	}
}

func TestImportingAShareGivesTheRecipientTheirOwnWebhookSecret(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	recipient := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)
	share, _ := createShare(t, d, wf.ID, owner, `{}`)

	sharerSaved, err := d.Store.GetWorkflow(context.Background(), wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	sharerSecret := secretOf(t, sharerSaved.Nodes, "n_trigger", "webhookSecret")
	if sharerSecret == "" {
		t.Fatal("the fixture should have a webhook secret to begin with")
	}

	imported := importShare(t, d, share.Token, recipient)
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), imported.ID) })

	recipientSaved, err := d.Store.GetWorkflow(context.Background(), imported.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipientSecret := secretOf(t, recipientSaved.Nodes, "n_trigger", "webhookSecret")

	if recipientSecret == "" {
		t.Fatal("a webhook trigger with no secret can never be fired -- ensureWebhookSecrets should have minted one")
	}
	if recipientSecret == sharerSecret {
		t.Error("sharer and recipient must not end up on the same webhook secret")
	}
	if recipientSaved.UserID != recipient {
		t.Errorf("the import belongs to the importer, got user %q", recipientSaved.UserID)
	}
	if recipientSaved.Status != models.WorkflowStatusDraft {
		t.Errorf("an imported workflow starts as a draft, got %q", recipientSaved.Status)
	}
	if recipientSaved.ScheduleCron != nil || recipientSaved.GeofenceLat != nil {
		t.Error("an import must not inherit the sharer's schedule or geofence")
	}
	if len(recipientSaved.Nodes) != 5 || len(recipientSaved.Edges) != 4 {
		t.Errorf("the imported graph should match the shared one, got %d nodes / %d edges",
			len(recipientSaved.Nodes), len(recipientSaved.Edges))
	}
}

// A hand-crafted code is the case the ordinary save path cannot defend
// against: encryptField returns an "enc:"-prefixed value untouched, and a
// freshly created workflow has no prior value to compare it with, so it would
// be stored verbatim and would decrypt with the server key on the next run.
func TestPastedGraphCannotPlantACredential(t *testing.T) {
	d := testDeps(t)

	body, _ := json.Marshal(map[string]any{
		"name": "Looks innocent",
		"nodes": []models.WorkflowNode{{
			ID: "n1", Type: models.NodeTypeProvider, Template: "openai",
			APIKey:  "enc:3q2+7wAAAAAAAAAAAAAAAA==",
			Secrets: map[string]string{"stripeSecretKey": "enc:3q2+7wAAAAAAAAAAAAAAAA=="},
		}},
		"edges": []models.WorkflowEdge{},
	})
	req := httptest.NewRequest(http.MethodPost, "/workflows/import", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, testUser(t, d)))
	w := httptest.NewRecorder()
	d.ImportWorkflowGraph(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("ImportWorkflowGraph = %d, want 201: %s", w.Code, w.Body.String())
	}

	var created models.Workflow
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), created.ID) })

	stored, err := d.Store.GetWorkflow(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range stored.Nodes {
		if n.APIKey != "" {
			t.Errorf("a pasted graph planted an API key: %q", n.APIKey)
		}
		if len(n.Secrets) > 0 {
			t.Errorf("a pasted graph planted secrets: %v", n.Secrets)
		}
	}
}

func TestARevokedLinkIsIndistinguishableFromOneThatNeverExisted(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	stranger := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	live, _ := createShare(t, d, wf.ID, owner, `{}`)
	if code, _ := readShare(t, d, live.Token); code != http.StatusOK {
		t.Fatalf("a fresh link should read 200, got %d", code)
	}

	revoked, _ := createShare(t, d, wf.ID, owner, `{}`)
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+revoked.Token, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, owner))
	req = withURLParam(req, "token", revoked.Token)
	w := httptest.NewRecorder()
	d.RevokeShare(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RevokeShare = %d, want 204: %s", w.Code, w.Body.String())
	}

	missingCode, missingBody := readShare(t, d, "a-token-that-was-never-minted")
	revokedCode, revokedBody := readShare(t, d, revoked.Token)
	if missingCode != http.StatusNotFound || revokedCode != http.StatusNotFound {
		t.Fatalf("missing = %d, revoked = %d; both must be 404", missingCode, revokedCode)
	}
	// Same status AND same body: a different message would tell a caller
	// which tokens have ever existed.
	if missingBody != revokedBody {
		t.Errorf("a revoked link answers %q but a missing one answers %q -- the difference is the leak",
			revokedBody, missingBody)
	}

	// Revoking somebody else's link is the same 404, and does not work.
	other := httptest.NewRequest(http.MethodDelete, "/shares/"+live.Token, nil)
	other = other.WithContext(context.WithValue(other.Context(), handlers.CtxUserID, stranger))
	other = withURLParam(other, "token", live.Token)
	ow := httptest.NewRecorder()
	d.RevokeShare(ow, other)
	if ow.Code != http.StatusNotFound {
		t.Errorf("a stranger revoking a link = %d, want 404", ow.Code)
	}
	if code, _ := readShare(t, d, live.Token); code != http.StatusOK {
		t.Error("a stranger's revoke attempt must not have killed the link")
	}
}

func TestAnExpiredLinkReadsAsGone(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)
	ctx := context.Background()

	// A link the handler makes expires in whole days at the earliest, so the
	// expired row is written straight through the store. What is under test
	// is that the read honours Live(), not the arithmetic that produced the
	// timestamp -- and the handler's own end of that is covered below.
	past := time.Now().UTC().Add(-time.Hour)
	expired, _, err := d.Store.CreateWorkflowShare(ctx, models.WorkflowShare{
		Token:      "expired-token-fixture-000001",
		WorkflowID: wf.ID,
		UserID:     owner,
		Name:       wf.Name,
		Graph:      models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeTrigger}}},
		NodeCount:  1,
		ExpiresAt:  &past,
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	code, expiredBody := readShare(t, d, expired.Token)
	if code != http.StatusNotFound {
		t.Fatalf("an expired link = %d, want 404", code)
	}
	_, missingBody := readShare(t, d, "a-token-that-was-never-minted")
	if expiredBody != missingBody {
		t.Errorf("an expired link answers %q but a missing one answers %q -- the difference is the leak",
			expiredBody, missingBody)
	}

	// The handler's half: a future expiry is stored, and the link still reads.
	live, _ := createShare(t, d, wf.ID, owner, `{"expiresInDays":7}`)
	if live.ExpiresAt == nil || !live.ExpiresAt.After(time.Now()) {
		t.Fatalf("expiresInDays should have set a future expiry, got %v", live.ExpiresAt)
	}
	if code, _ := readShare(t, d, live.Token); code != http.StatusOK {
		t.Error("a link expiring in a week should read today")
	}

	// And the sweep only takes what has actually expired.
	removed, err := d.Store.SweepExpiredWorkflowShares(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if removed < 1 {
		t.Error("the sweep should have removed the expired row")
	}
	if code, _ := readShare(t, d, live.Token); code != http.StatusOK {
		t.Error("the sweep must not remove a link that has not expired yet")
	}
}

// A link is the same link only while it still describes the same workflow.
//
// The regression test for the defect that prompted all of this. A share
// snapshot is frozen when it is created, so a dialog that hands an existing
// link straight back hands back a link to an OLDER graph -- which is how an
// imported copy arrived one connection short of the original. Reuse has to be
// conditional on the snapshot, and the condition belongs here rather than in
// the dialog, which has no way to compare sanitised graphs.
func TestAReusedLinkIsHandedBackOnlyWhileTheSnapshotMatches(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	code, first, redactions := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
	if code != http.StatusCreated {
		t.Fatalf("the first share = %d, want 201", code)
	}

	// Nothing has changed, so the same link comes back -- and 200, because
	// nothing was created. Opening the dialog repeatedly must not leave a
	// trail of links behind.
	code, again, againRedactions := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
	if code != http.StatusOK || again.Token != first.Token {
		t.Fatalf("reopening gave %d %s, want 200 and the same token %s", code, again.Token, first.Token)
	}
	// The counts come back on the reuse path too. They are what the dialog
	// prints as "not included: 2 API keys, your webhook secret ..." -- the
	// one moment the sharer can still change their mind -- so a reused link
	// going quiet about them would be its own small defect.
	if againRedactions != redactions {
		t.Errorf("a reused link reported %+v, want the same %+v", againRedactions, redactions)
	}

	// One more connection, and the existing link no longer describes this
	// workflow. It must not be offered again.
	extra := append(shareFixtureEdges(), models.WorkflowEdge{
		ID: "e5", From: "n_trigger", To: "n_email", Kind: models.EdgeKindFlow,
	})
	saveSharedWorkflow(t, d, wf.ID, owner, extra)

	code, third, _ := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
	if code != http.StatusCreated {
		t.Fatalf("sharing an edited workflow = %d, want 201 -- a new link", code)
	}
	if third.Token == first.Token {
		t.Fatal("an edited workflow was given back the link to its previous version")
	}
	if third.EdgeCount != first.EdgeCount+1 {
		t.Errorf("the new link holds %d edges, want %d -- the edit did not reach the snapshot",
			third.EdgeCount, first.EdgeCount+1)
	}

	// The old link keeps working and keeps its own older snapshot: somebody
	// already holding it was promised a frozen copy, not a moving one.
	if statusCode, _ := readShare(t, d, first.Token); statusCode != http.StatusOK {
		t.Error("creating a newer link must not disturb one already handed out")
	}
}

// Requests arriving together still leave one link.
//
// Not hypothetical. React's development double-invoke fires the dialog's
// effect twice, milliseconds apart, and an earlier version answered reuse
// from a query the HANDLER ran before calling the store -- so between finding
// nothing and inserting there sat a token mint and a whole transaction's
// worth of round trips. Both calls looked, both found nothing, and a workflow
// came away with two links to the same graph the first time it was ever
// shared.
//
// What this test pins is that behaviour: a lookup that far outside the insert
// it guards loses this race readily. The lookup now runs inside the insert's
// own transaction, which shrinks the window to a fraction of a millisecond,
// and an advisory lock on the workflow closes what is left. Be honest about
// the limit: with the lookup already inside the transaction, this test passes
// with the lock removed too -- the remaining window is too narrow to hit on
// demand. The lock is reasoned correctness, not something measured here.
func TestSharingTwiceAtOnceStillLeavesOneLink(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	const attempts = 8
	tokens := make(chan string, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, share, _ := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
			tokens <- share.Token
		}()
	}
	close(start)
	wg.Wait()
	close(tokens)

	distinct := map[string]bool{}
	for tok := range tokens {
		distinct[tok] = true
	}
	if len(distinct) != 1 {
		t.Fatalf("%d concurrent shares produced %d different links, want 1", attempts, len(distinct))
	}

	shares, err := d.Store.ListWorkflowShares(context.Background(), wf.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 {
		t.Errorf("the workflow ended up with %d rows, want 1", len(shares))
	}
}

// The description reaches the recipient's own copy.
//
// It is snapshotted beside the graph on purpose, and the preview shows it to
// the recipient before they decide -- and then the import used to pass only
// the name, so the one sentence saying what the workflow is for vanished at
// the moment they accepted it.
func TestImportKeepsTheDescriptionTheRecipientWasShown(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	recipient := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	description := "Screens inbound CVs and posts the shortlist to Slack."
	if _, err := d.Store.UpdateWorkflowAndDescription(
		context.Background(), wf.ID, wf.Name,
		models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges},
		&description,
	); err != nil {
		t.Fatal(err)
	}

	share, _ := createShare(t, d, wf.ID, owner, `{}`)
	if share.Description != description {
		t.Fatalf("the snapshot carries %q, want the workflow's own description", share.Description)
	}

	imported := importShare(t, d, share.Token, recipient)
	if imported.Description != description {
		t.Errorf("the imported workflow has description %q, want %q", imported.Description, description)
	}
}

// A link to an empty canvas is not worth handing anybody.
//
// The recipient opens it, is told it is "ready to run as-is", imports it, and
// has an empty workflow. ImportWorkflowGraph has always refused a graph with
// no nodes; this end had no such guard.
func TestAnEmptyWorkflowCannotBeShared(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)

	wf, err := d.Store.CreateWorkflow(context.Background(), "Nothing yet", owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	code, _, _ := postShare(t, d, wf.ID, owner, `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("sharing an empty workflow = %d, want 400", code)
	}

	shares, err := d.Store.ListWorkflowShares(context.Background(), wf.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 0 {
		t.Errorf("a refused share still left %d rows behind", len(shares))
	}
}

// Reuse must never resurrect a link the sharer has already retracted, and
// must never answer a request for an expiring link with a permanent one.
func TestReuseSkipsRevokedLinksAndExpiryRequests(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	_, revoked, _ := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
	req := httptest.NewRequest(http.MethodDelete, "/shares/"+revoked.Token, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, owner))
	req = withURLParam(req, "token", revoked.Token)
	w := httptest.NewRecorder()
	d.RevokeShare(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RevokeShare = %d, want 204", w.Code)
	}

	code, fresh, _ := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true}`)
	if code != http.StatusCreated || fresh.Token == revoked.Token {
		t.Fatalf("sharing after a revoke gave %d %s -- the retracted link came back",
			code, fresh.Token)
	}

	// An expiring link is a different promise from a permanent one, so asking
	// for one mints its own row rather than reusing the link just made.
	code, expiring, _ := postShare(t, d, wf.ID, owner, `{"reuseIfUnchanged":true,"expiresInDays":7}`)
	if code != http.StatusCreated || expiring.Token == fresh.Token {
		t.Fatalf("asking for a 7-day link gave %d %s -- a never-expiring link was reused",
			code, expiring.Token)
	}
	if expiring.ExpiresAt == nil {
		t.Error("the link asked to expire has no expiry")
	}
}

func TestOnlyTheOwnerCanShareAWorkflow(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/share", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, testUser(t, d)))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.CreateShare(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("a stranger sharing somebody else's workflow = %d, want 404", w.Code)
	}
}

func TestPublicReadHidesHowManyPeopleTookACopy(t *testing.T) {
	d := testDeps(t)
	owner := testUser(t, d)
	wf := seedSharedWorkflow(t, d, owner)
	share, _ := createShare(t, d, wf.ID, owner, `{}`)

	imported := importShare(t, d, share.Token, testUser(t, d))
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), imported.ID) })

	_, body := readShare(t, d, share.Token)
	var out struct {
		Share        models.WorkflowShare             `json:"share"`
		Requirements handlers.ShareImportRequirements `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Share.ImportCount != 0 {
		t.Errorf("the public read exposed an import count of %d", out.Share.ImportCount)
	}

	// The sharer's own listing is where that number belongs.
	lreq := httptest.NewRequest(http.MethodGet, "/workflows/"+wf.ID+"/shares", nil)
	lreq = lreq.WithContext(context.WithValue(lreq.Context(), handlers.CtxUserID, owner))
	lreq = withURLParam(lreq, "id", wf.ID)
	lw := httptest.NewRecorder()
	d.ListWorkflowShares(lw, lreq)
	if lw.Code != http.StatusOK {
		t.Fatalf("ListWorkflowShares = %d: %s", lw.Code, lw.Body.String())
	}
	var listed struct {
		Shares []models.WorkflowShare `json:"shares"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range listed.Shares {
		if s.Token != share.Token {
			continue
		}
		found = true
		if s.ImportCount != 1 {
			t.Errorf("the sharer should see 1 import, got %d", s.ImportCount)
		}
		if len(s.Graph.Nodes) != 0 {
			t.Error("the listing is about the links, not their contents")
		}
	}
	if !found {
		t.Error("the sharer's own listing did not include the link they just made")
	}

	// And the recipient is told what they have to supply: the BYOK provider
	// key, and the file whose bytes were stripped.
	if out.Requirements.APIKeys != 1 || out.Requirements.Files != 1 {
		t.Errorf("requirements shown to the recipient look wrong: %+v", out.Requirements)
	}
}

// --- helpers ---------------------------------------------------------------

// testUser creates a real row in users and returns its id.
//
// Not the literal "dev" that workflows_test.go uses. That works there only
// because workflows.user_id has no foreign key -- it is a bare TEXT column
// with DEFAULT 'dev' from migration 000001 -- whereas workflow_shares.user_id
// REFERENCES users(id) ON DELETE CASCADE, so a share owned by an id with no
// user behind it cannot be inserted at all. CI caught exactly that.
//
// Keeping the constraint and fixing the tests, rather than the other way
// round: in production a workflow's owner always comes from the JWT's subject
// and so is always a real users row, and the cascade is what retracts
// somebody's published links when their account goes away.
func testUser(t *testing.T, d *handlers.Deps) string {
	t.Helper()
	email := fmt.Sprintf("share-test-%d@example.invalid", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func readShare(t *testing.T, d *handlers.Deps, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/shares/"+token, nil)
	req = withURLParam(req, "token", token)
	w := httptest.NewRecorder()
	d.GetShare(w, req)
	return w.Code, w.Body.String()
}

func importShare(t *testing.T, d *handlers.Deps, token, userID string) models.Workflow {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/shares/"+token+"/import", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "token", token)
	w := httptest.NewRecorder()
	d.ImportShare(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("ImportShare = %d, want 201: %s", w.Code, w.Body.String())
	}
	var wf models.Workflow
	if err := json.Unmarshal(w.Body.Bytes(), &wf); err != nil {
		t.Fatal(err)
	}
	return wf
}

// secretOf reads one stored secret, decrypting it the way the engine would.
func secretOf(t *testing.T, nodes []models.WorkflowNode, nodeID, key string) string {
	t.Helper()
	decrypted := handlers.DecryptNodes(nodes, testEncryptionKey)
	for _, n := range decrypted {
		if n.ID == nodeID {
			return n.Secrets[key]
		}
	}
	return ""
}
